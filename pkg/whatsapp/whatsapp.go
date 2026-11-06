package whatsapp

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"wppserver/pkg/config"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	uuid "github.com/google/uuid"
	_ "github.com/mdp/qrterminal/v3"
	_ "github.com/skip2/go-qrcode"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Device struct {
	Id             uuid.UUID `sql:"id;default:uuid_generate_v4()"`
	UserId         uuid.UUID `sql:"userid;default:uuid_generate_v4()"`
	Jid            types.JID `json:"jid"`
	Connected      string    `json:"connected"`
	QrCode         dataQrCode
	Client         *whatsmeow.Client
	EventHandlerID uint32
	FnEvent        func(d *Device, rawEvt interface{})
	FnUpdateDevice func(d *Device)
}

type dataQrCode struct {
	Base64QrCode string `json:"base64qrcode"`
	Expiration   string `json:"expiration"`
}

// ParseJID validates a recipient and converts it to a WhatsApp JID.
// Invalid input is never fatal: the caller receives ok=false.
func (d *Device) ParseJID(arg string) (types.JID, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return types.NewJID("", types.DefaultUserServer), false
	}
	if arg[0] == '+' {
		arg = arg[1:]
	}

	if arg == "" {
		return types.NewJID("", types.DefaultUserServer), false
	}

	// Basic only digit check for recipient phone number, we want to remove @server and .session
	phonenumber := strings.Split(arg, "@")[0]
	phonenumber = strings.Split(phonenumber, ".")[0]

	for _, c := range phonenumber {
		if c < '0' || c > '9' {
			log.Printf("Bad jid format, return empty: %v\n", phonenumber)
			recipient, _ := types.ParseJID("")
			return recipient, false
		}
	}

	if !strings.ContainsRune(arg, '@') {
		return types.NewJID(arg, types.DefaultUserServer), true
	}

	recipient, err := types.ParseJID(arg)
	if err != nil {
		log.Printf("Invalid jid: %v\n", err)
		return recipient, false
	}
	if recipient.User == "" {
		log.Printf("Invalid jid. No server specified: %v\n", arg)
		return recipient, false
	}

	return recipient, true
}

func (d *Device) StartClient(container *sqlstore.Container) *whatsmeow.Client {
	if container == nil {
		log.Printf("Cannot start WhatsApp client: session store is unavailable")
		d.Client = nil
		return nil
	}

	var deviceStore *store.Device

	log.Printf("Starting connection to Whatsapp:  %v\n", d.UserId)

	if d.Jid.String() != "" {
		var err error
		deviceStore, err = container.GetDevice(context.Background(), d.Jid)
		if err != nil {
			log.Printf("Failed to load WhatsApp device %s: %v\n", d.Jid, err)
			d.Client = nil
			return nil
		}
	}

	if deviceStore == nil {
		log.Print("No valid jid found. Creating new device")
		deviceStore = container.NewDevice()
	}

	store.DeviceProps.PlatformType = waProto.DeviceProps_UNKNOWN.Enum()
	store.DeviceProps.Os = proto.String("RMS Receipt Sender")

	clientLog := waLog.Stdout("Client", logLevel(), true)

	d.Client = whatsmeow.NewClient(deviceStore, clientLog)
	d.EventHandlerID = d.Client.AddEventHandler(d.eventHandlerDevices)

	return d.Client
}

func (d *Device) eventHandlerDevices(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.AppStateSyncComplete:
		if len(d.Client.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
			err := d.Client.SendPresence(context.Background(), types.PresenceAvailable)
			if err != nil {
				log.Printf("Failed to send available presence: %v\n", err)
			} else {
				log.Print("Marked self as available")
			}
		}
	case *events.Connected, *events.PushNameSetting:
		if len(d.Client.Store.PushName) == 0 {
			return
		}
		err := d.Client.SendPresence(context.Background(), types.PresenceAvailable)
		if err != nil {
			log.Printf("Failed to send available presence: %v\n", err)
		} else {
			log.Print("Marked self as available")
		}
	case *events.PairSuccess:
		d.Jid = evt.ID
		log.Printf("New jid: %v\n", d.Jid)
		if d.FnUpdateDevice != nil {
			d.FnUpdateDevice(d)
		}
	}

	//d.FnEvent(d, rawEvt)
}

func (d *Device) DisconnectClient() {
	if d == nil || d.Client == nil {
		return
	}

	log.Printf("Disconnecting to Whatsapp:  %v\n", d)

	if d.Client.IsConnected() {
		d.Client.Disconnect()
	}
}

type Devices struct {
	AllDevices map[uuid.UUID]*Device
	Container  *sqlstore.Container
}

func NewDevices() (*Devices, error) {
	cfg := config.GetConfig()
	myDevices := &Devices{
		AllDevices: make(map[uuid.UUID]*Device),
	}

	dsn := cfg.DB.DbURI
	if strings.HasPrefix(cfg.DB.Dialect, "sqlite") &&
		!strings.Contains(dsn, "_pragma=") &&
		!strings.Contains(dsn, "_foreign_keys=") {
		if strings.Contains(dsn, "?") {
			dsn += "&_pragma=foreign_keys(1)"
		} else {
			dsn += "?_pragma=foreign_keys(1)"
		}
	}

	dbLog := waLog.Stdout("Database", logLevel(), false)

	container, err := sqlstore.New(context.Background(), cfg.DB.Dialect, dsn, dbLog)
	if err != nil {
		return nil, fmt.Errorf("open whatsapp session store: %w", err)
	}

	myDevices.Container = container

	return myDevices, nil
}

func (ds *Devices) RegisterNew(dUUID uuid.UUID) (*Device, bool) {
	myDevice := Device{}

	id, err := uuid.NewRandom()
	if err != nil {
		log.Printf("Failed to create device id: %v\n", err)
		return nil, false
	}

	myDevice.Id = id
	myDevice.UserId = dUUID

	ds.Add(&myDevice)

	return ds.Get(dUUID)
}

func (ds *Devices) Get(did uuid.UUID) (*Device, bool) {
	if _, ok := ds.AllDevices[did]; ok {
		return ds.AllDevices[did], true
	}
	return nil, false
}

func (ds *Devices) GetBy(sType string, value string) (*Device, bool) {
	for _, d := range ds.AllDevices {
		if sType == "phone" {
			phone := strings.Split(d.Jid.String(), "@")[0]
			phone = strings.Split(phone, ".")[0]

			if value == phone {
				return d, true
			}
		} else if sType == "jid" {
			if value == d.Jid.String() {
				return d, true
			}
		}
	}
	return nil, false
}

func (ds *Devices) Add(d *Device) {
	ds.AllDevices[d.UserId] = d
}

func (ds *Devices) Remove(did uuid.UUID) bool {
	d, ok := ds.AllDevices[did]
	if !ok {
		return true
	}

	d.DisconnectClient()
	delete(ds.AllDevices, did)

	_, stillThere := ds.AllDevices[did]

	return !stillThere
}

// logLevel keeps WhatsApp protocol debug output off by default. The old
// default leaked QR/session details into the logs and produced huge log files.
func logLevel() string {
	level := strings.ToUpper(strings.TrimSpace(os.Getenv("WHATSAPP_LOG_LEVEL")))
	switch level {
	case "DEBUG", "INFO", "WARN", "ERROR":
		return level
	default:
		return "INFO"
	}
}
