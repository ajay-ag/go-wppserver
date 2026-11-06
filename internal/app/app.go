package app

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"wppserver/pkg/config"
	"wppserver/pkg/http/model"
	"wppserver/pkg/utils"
	"wppserver/pkg/whatsapp"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "modernc.org/sqlite"
)

// App has router and db instances
type App struct {
	DB      *sql.DB
	Router  *mux.Router
	Devices *whatsapp.Devices
	Config  *config.Config
}

// App initialize with predefined configuration
func (a *App) Initialize(config *config.Config) error {
	var err error

	if a.DB, err = a.DatabaseSetup(config); err != nil {
		return err
	}

	a.Config = config

	devices, err := whatsapp.NewDevices()
	if err != nil {
		return err
	}

	a.Devices = devices
	a.connectDevices()
	a.Router = mux.NewRouter()
	a.setRouters()

	if err := a.Run(config); err != nil {
		return err
	}

	return nil
}

// Initial database settings
func (a *App) DatabaseSetup(config *config.Config) (*sql.DB, error) {
	// Create the sqlite data directory. The whatsmeow store creates its own
	// tables on first run; the legacy dbdata.zip seed is no longer required.
	if strings.HasPrefix(config.DB.DbURI, "file:dbdata/") {
		path, err := utils.GetWorkDir()
		if err != nil {
			return nil, err
		}
		log.Printf("Work dir %v", path)

		if err := os.MkdirAll(path+"/dbdata", 0o755); err != nil {
			return nil, err
		}
	}

	// Open database connection
	db, err := sql.Open(config.DB.Dialect, config.DB.DbURI)
	if err != nil {
		return nil, err
	}
	err = db.Ping()
	if err != nil {
		return nil, err
	}

	// Create initial tables in db
	for _, v := range []string{
		"CREATE TABLE IF NOT EXISTS wppserver_users( id VARCHAR(36) NOT NULL, name	TEXT, email	TEXT, type	TEXT, password	TEXT, status	TEXT DEFAULT 'disabled', PRIMARY KEY(id) );",
		"CREATE TABLE IF NOT EXISTS wppserver_apikeys( id VARCHAR(36) NOT NULL, userid VARCHAR(36) NOT NULL, description TEXT, permissions TEXT, clientid TEXT, clientsecret TEXT, truncatedsecret	TEXT, scope	TEXT, status TEXT DEFAULT 'enabled', jid TEXT, PRIMARY KEY(id) );",
		"CREATE TABLE IF NOT EXISTS wppserver_devices( id VARCHAR(36) NOT NULL, userid VARCHAR(36), jid	TEXT, qrcode	TEXT, connected	TEXT, PRIMARY KEY(id) );",
		"CREATE TABLE IF NOT EXISTS wppserver_webhooks( id VARCHAR(36) NOT NULL, deviceid VARCHAR(36), description	TEXT, status	TEXT, url	TEXT, events	TEXT, secret	TEXT, PRIMARY KEY(id) );",
	} {
		if _, err := db.Exec(v); err != nil {
			return nil, err
		}
	}

	// Create initial admin user
	if config.AUTH.UserEmail != "" && config.AUTH.UserPassword != "" {
		user := model.User{}
		err := db.QueryRow("SELECT id, name, email, type, status FROM wppserver_users WHERE type='admin'").Scan(
			&user.Id, &user.Name, &user.Email, &user.Type, &user.Status)
		if err == sql.ErrNoRows {
			stmt, err := db.Prepare("INSERT INTO wppserver_users(id, name, email, password, type, status) VALUES($1,$2,$3,$4,$5,$6);")
			if err != nil {
				return nil, err
			}

			user.Id, err = uuid.NewUUID()
			user.Password, err = utils.HashPassword(config.AUTH.UserPassword)
			if err != nil {
				return nil, err
			}

			_, err = stmt.Exec(user.Id, "root", config.AUTH.UserEmail, user.Password, "admin", "enabled")
			if err != nil {
				return nil, err
			} else {
				log.Printf("Initial admin user registed %q\n", user.Id)
			}
		}
	}

	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	return db, nil
}

// Connect devices saved in DB
func (a *App) connectDevices() {
	log.Printf("Init connection to Whatsapp")

	rows, err := a.DB.Query("SELECT id, userid, jid, connected FROM wppserver_devices WHERE connected='yes'")
	if err != nil {
		log.Printf("query error: %v\n", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var device whatsapp.Device

		if err := rows.Scan(&device.Id, &device.UserId, &device.Jid, &device.Connected); err != nil {
			log.Printf("rows error: %v\n", err)
			continue
		}

		if _, ok := a.Devices.Get(device.UserId); ok {
			continue
		}

		log.Printf("Connect to Whatsapp on startup:  %v\n", device.Jid)

		device.FnUpdateDevice = func(d *whatsapp.Device) {
			utils.DBUpdateDevice(a.DB, d)
		}

		device.FnEvent = a.webhook
		if device.StartClient(a.Devices.Container) == nil {
			log.Printf("Skipping device %s: could not start WhatsApp client\n", device.Id)
			continue
		}

		if err := device.Client.Connect(); err != nil {
			log.Printf("Could not reconnect device %s: %v\n", device.Id, err)
			continue
		}

		if device.Client.Store.ID != nil {
			a.Devices.Add(&device)
		} else {
			a.Devices.Remove(device.UserId)
			utils.DBDeleteDevice(a.DB, &device)
		}
	}

	if err := rows.Err(); err != nil {
		log.Printf("query error: %v\n", err)
	}
}

// Set all required routers
func (a *App) setRouters() {
	a.Docs("/docs")
	a.Get("/healthz", a.health)

	a.Post("/v1/auth", a.accessToken)
	a.Get("/v1/auth/refresh", a.refreshToken)

	a.Post("/v1/user", a.registerUser)
	a.Get("/v1/user", a.getUser)
	a.Put("/v1/user", a.updateUser)
	a.Delete("/v1/user", a.deleteUser)

	a.Put("/v1/user/status", a.statusUser)
	a.Put("/v1/user/password", a.passwordUser)

	a.Post("/v1/user/key", a.createUserKey)
	a.Delete("/v1/user/key", a.deleteUserKey)
	a.Get("/v1/user/key", a.getUserKeys)

	a.Get("/v1/users", a.getAllUsers)
	a.Get("/v1/users/findusers", a.findUsers)

	a.Get("/v1/device/login", a.loginDevice)
	a.Get("/v1/device/logout", a.logoutDevice)

	a.Get("/v1/session/status", a.status)
	a.Post("/v1/session/connect", a.connect)
	a.Post("/v1/session/disconnect", a.disconnect)

	//a.Post("/v1/webhook", a.createWebhook)
	//a.Get("/v1/webhook", a.deleteWebhook)
	//a.Get("/v1/webhook", a.getWebhooks)

	a.Post("/v1/chat/send/text", a.sendText)
	a.Post("/v1/chat/send/image", a.sendImage)
	a.Post("/v1/chat/send/document", a.sendDocument)

	// Contact scraping is intentionally disabled: this service only sends
	// receipt messages and should not expose address-book data.
	// a.Post("/v1/phone/scraping", a.scrapingPhones)
	// a.Get("/v1/phone/contacts", a.getContacts)
}

// Health is intentionally unauthenticated so Docker/systemd can probe the
// process without exposing session details.
func (a *App) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// Show application docs
func (a *App) Docs(path string) {
	a.Router.PathPrefix(path).Handler(http.StripPrefix(path, http.FileServer(http.Dir("./docs/"))))
}

// Wrap the router for GET method
func (a *App) Get(path string, f func(w http.ResponseWriter, r *http.Request)) {
	a.Router.HandleFunc(path, f).Methods("GET")
}

// Wrap the router for POST method
func (a *App) Post(path string, f func(w http.ResponseWriter, r *http.Request)) {
	a.Router.HandleFunc(path, f).Methods("POST")
}

// Wrap the router for PUT method
func (a *App) Put(path string, f func(w http.ResponseWriter, r *http.Request)) {
	a.Router.HandleFunc(path, f).Methods("PUT")
}

// Wrap the router for DELETE method
func (a *App) Delete(path string, f func(w http.ResponseWriter, r *http.Request)) {
	a.Router.HandleFunc(path, f).Methods("DELETE")
}

// Run the app on it's router
func (a *App) Run(config *config.Config) error {
	if config.TLS.CertFile != "" && config.TLS.KeyFile != "" {
		log.Printf("Listening for TCP addr network: %v\n. Accepting connections over TLS.", config.SERVER.Host)
		if err := http.ListenAndServeTLS(
			config.SERVER.Host, config.TLS.CertFile, config.TLS.KeyFile, a.Router); err != nil {
			return err
		}
	} else {
		log.Printf("Listening for TCP addr network: %v\n", config.SERVER.Host)
		if err := http.ListenAndServe(config.SERVER.Host, a.Router); err != nil {
			return err
		}
	}

	return nil
}
