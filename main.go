package main

import (
	"time"
	"sync"
	"net"
	"net/http"
	"log"
	"github.com/songgao/water"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  2048,
	WriteBufferSize: 2048,
	CheckOrigin: func(r *http.Request) bool { return true },
}

var slotMutex sync.Mutex
var usedSlots map[uint64]bool = make(map[uint64]bool)

func main() {
	http.HandleFunc("/", serveWs)
	err := http.ListenAndServe("127.0.0.1:9000", nil)
	if err != nil {
		panic(err)
	}
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	iface, err := water.New(water.Config{
		DeviceType: water.TUN,
	})
	if err != nil {
		log.Println(err)
		return
	}

	packet := make([]byte, 2000)

	var slot uint64 = 0
	slotMutex.Lock()
	for usedSlots[slot] {
		slot = slot + 2
	}
	usedSlots[slot] = true
	slotMutex.Unlock()

	defer func() {
		slotMutex.Lock()
		delete(usedSlots, slot)
		slotMutex.Unlock()
		iface.Close()
	}()

	ipServer := net.IPv4(10, byte((slot << 16) & 0xFF), byte((slot << 8) & 0xFF), byte(slot & 0xFF)).String()
	slotB := slot + 1
	ipClient := net.IPv4(10, byte((slotB << 16) & 0xFF), byte((slotB << 8) & 0xFF), byte(slotB & 0xFF)).String()

	err = configIface(iface.Name(), ipClient, ipServer)
	if err != nil {
		log.Println(err)
		return
	}

	keepAlive(conn)

	go func() {
		for {
			n, err := iface.Read(packet)
			if err != nil {
				log.Println(err)
				conn.Close()
				break
			}
			w, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				break
			}
			w.Write(packet[:n])
			err = w.Close()
			if err != nil {
				break
			}
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Println(err)
			}
			break
		}
		iface.Write(msg)
	}
}

func keepAlive(c *websocket.Conn) {
	timeout := time.Duration(30) * time.Second

	lastResponse := time.Now()
	c.SetPongHandler(func(msg string) error {
		lastResponse = time.Now()
		return nil
	})

	go func() {
		for {
			err := c.WriteMessage(websocket.PingMessage, []byte("keepalive"))
			if err != nil {
				return
			}
			time.Sleep(timeout/2)
			if(time.Now().Sub(lastResponse) > timeout) {
				c.Close()
				return
			}
		}
	}()
}
