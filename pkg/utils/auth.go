package utils

import (
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"wppserver/pkg/http/model"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	signingKeyOnce  sync.Once
	signingKeyValue string
)

// signingKey is loaded lazily from AUTH_JWT_SECRET. It must be lazy because
// .env is parsed in main(), after package-level initialisation. If the value
// is missing (or still the example value), an ephemeral secret is generated
// so the service never ships with a publicly known signing key.
func signingKey() string {
	signingKeyOnce.Do(func() {
		key := strings.TrimSpace(os.Getenv("AUTH_JWT_SECRET"))
		if key != "" && key != "change-this-to-any-random-text" {
			signingKeyValue = key
			return
		}

		log.Print("WARNING: AUTH_JWT_SECRET is not set (or is the example value). Generating an ephemeral secret; tokens will not survive a restart.")

		buf := make([]byte, 32)
		if _, err := cryptorand.Read(buf); err != nil {
			log.Fatalf("could not generate JWT secret: %v", err)
		}

		signingKeyValue = base64.RawURLEncoding.EncodeToString(buf)
	})

	return signingKeyValue
}

func tokenTTL() time.Duration {
	minutes := 60

	if raw := strings.TrimSpace(os.Getenv("AUTH_TOKEN_TTL_MINUTES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			minutes = parsed
		}
	}

	return time.Duration(minutes) * time.Minute
}

func MakeRandomNumber(min int, max int) int {
	rand.Seed(time.Now().UTC().UnixNano())
	return rand.Intn(max-min) + min
}

func MakeRandomString() string {
	uuid, err := uuid.NewRandom()

	if err != nil {
		panic(err)
	}

	return strings.Replace(uuid.String(), "-", "", -1)
}

func MakeHMAC256(message string) string {
	hmacSecret := []byte(signingKey())

	h := hmac.New(sha256.New, hmacSecret)
	h.Write([]byte(message))

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func Message(status bool, message string) map[string]interface{} {
	return map[string]interface{}{"status": status, "message": message}
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func ExtractClaims(tokenStr string) (jwt.MapClaims, bool) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return []byte(signingKey()), nil
	})

	if err != nil {
		return nil, false
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, true
	}

	log.Printf("Invalid JWT Token")
	return nil, false
}

func MakeTokenFromUUID(uuid uuid.UUID) string {
	expirationTime := time.Now().Add(tokenTTL())

	claims := &model.Claims{
		ID: uuid,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(signingKey()))

	if err != nil {
		panic(err)
	}

	return tokenString
}

func MakeToken(userId uuid.UUID, escope string, args ...uuid.UUID) string {
	apiKeyId := uuid.Nil
	if len(args) != 0 {
		apiKeyId = args[0]
	}

	expirationTime := time.Now().Add(tokenTTL())
	claims := model.Claims{
		ApiKeyId: apiKeyId,
		UserId:   userId,
		Scope:    escope,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(signingKey()))

	if err != nil {
		log.Panic(err)
	}

	return tokenString
}

func GetRequestAuth(db *sql.DB, r *http.Request) (model.Auth, bool) {
	auth := model.Auth{}

	if err := r.ParseForm(); err != nil {
		log.Printf("could not parse request form: %v", err)
		return auth, false
	}

	reqToken := r.Header.Get("Authorization")
	splitToken := strings.Split(reqToken, "Bearer ")
	if len(splitToken) > 1 {
		reqToken = splitToken[1]
	}

	userAuthorization, okGetUserFromClaims := GetUserFromClaims(reqToken, db)
	if !okGetUserFromClaims {
		return auth, false
	}
	scopeAuthorization, okGetScopesFromClaims := GetScopeFromClaims(reqToken, db)
	if !okGetScopesFromClaims {
		return auth, false
	}

	auth.User = userAuthorization
	auth.Scope = scopeAuthorization

	return auth, true
}

func GetUserFromClaims(reqToken string, db *sql.DB) (model.User, bool) {
	user := model.User{}

	claims, okExtractUUID := ExtractClaims(reqToken)
	if !okExtractUUID {
		return user, false
	}

	rawUserID, ok := claims["userid"].(string)
	if !ok {
		return user, false
	}

	userUUID, errUUID := uuid.Parse(rawUserID)
	if errUUID != nil {
		return user, false
	}

	err := db.QueryRow("SELECT id, name, email, password, type, status FROM wppserver_users WHERE id=$1", userUUID).Scan(
		&user.Id, &user.Name, &user.Email, &user.Password, &user.Type, &user.Status)

	if err != nil {
		log.Printf("query error: %v\n", err)
		return user, false
	}

	return user, true
}

func GetScopeFromClaims(reqToken string, db *sql.DB) (model.Scope, bool) {
	scope := model.Scope{}

	claims, okExtractUUID := ExtractClaims(reqToken)
	if !okExtractUUID {
		return scope, false
	}

	rawScope, ok := claims["Scope"].(string)
	if !ok {
		return scope, false
	}

	scope.List = strings.Split(rawScope, " ")

	return scope, true
}

func GetRequestToken(db *sql.DB, r *http.Request) (string, bool) {
	var userId uuid.UUID
	var scope string
	var token string

	if err := r.ParseForm(); err != nil {
		log.Printf("could not parse request form: %v", err)
		return "", false
	}

	ctx := context.Background()
	granType := r.Form.Get("grant_type")

	if granType == "client" {

		clientId := r.Form.Get("client_id")
		clientSecret := r.Form.Get("client_secret")

		err := db.QueryRowContext(ctx, "SELECT userid, scope FROM wppserver_apikeys WHERE clientid=$1 AND clientsecret=$2 AND status='enabled'", clientId, MakeHMAC256(clientSecret)).Scan(
			&userId, &scope)

		if err != nil {
			return "", false
		}

		token = MakeToken(userId, scope)
		return token, true

	} else if granType == "password" || granType == "" {

		user := model.User{}
		username := r.Form.Get("username")
		password := r.Form.Get("password")

		err := db.QueryRowContext(ctx, "SELECT id, password, type FROM wppserver_users WHERE email=$1 AND status='enabled'", username).Scan(
			&user.Id, &user.Password, &user.Type)

		if err != nil {
			return "", false
		}

		match := CheckPasswordHash(password, user.Password)
		if !match {
			return "", false
		}

		if user.IsAdmin() {
			token = MakeToken(user.Id, "admin whatsapp")
		} else {
			token = MakeToken(user.Id, "agent whatsapp")
		}

		return token, true
	}

	return "", false
}

func BasicAuthRequest(db *sql.DB, r *http.Request) (model.Key, bool) {
	key := model.Key{}

	ck, cs, ok := r.BasicAuth()
	if !ok {
		return key, false
	}

	err := db.QueryRow("SELECT userid FROM wppserver_apikeys WHERE clientid=$1 AND clientsecret=$2 AND status='enabled'", ck, MakeHMAC256(cs)).Scan(
		&key.UserId)

	if err != nil {
		return key, false
	}

	return key, true
}
