package utils

import (
	"database/sql"
	"log"
	"wppserver/pkg/whatsapp"
)

// Update device in db
func DBUpdateDevice(db *sql.DB, device *whatsapp.Device) {
	if device == nil || device.Client == nil {
		return
	}

	if device.Client.IsConnected() {
		device.Connected = "yes"
	} else {
		device.Connected = "no"
	}
	_, err := db.Exec("UPDATE wppserver_devices SET id=$1, userid=$2, jid=$3, connected=$4, qrcode=$5 WHERE id=$1;",
		device.Id, device.UserId, device.Jid, device.Connected, device.QrCode.Base64QrCode)
	if err != nil {
		log.Printf("error: %v\n", err)
	} else {
		log.Printf("Device updated success %q\n", device.Jid)
	}
}

// Delete device in db
func DBDeleteDevice(db *sql.DB, device *whatsapp.Device) {
	_, err := db.Exec("DELETE FROM wppserver_devices WHERE id=$1 AND userid=$2;", device.Id, device.UserId)
	if err != nil {
		log.Printf("query exec error: %v\n", err)
		return
	} else {
		log.Printf("Device deleted success %q\n", device.Jid)
	}
}
