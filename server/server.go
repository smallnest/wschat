package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", ":8080", "http service address")

type Client struct {
	conn     *websocket.Conn
	nick     string
	sendChan chan string
}

func (c Client) startSend() {
	for msg := range c.sendChan {
		c.conn.WriteMessage(websocket.TextMessage, []byte(msg))
	}
}

type ChatState struct {
	clientsLock sync.RWMutex
	clients     map[*websocket.Conn]*Client
	numClients  int
}

var chatState = &ChatState{
	clients: make(map[*websocket.Conn]*Client),
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "home.html")
}

func main() {
	flag.Parse()

	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(chatState, w, r)
	})
	err := http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func serveWs(chatState *ChatState, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{conn: conn, sendChan: make(chan string, 256)}
	client.nick = fmt.Sprintf("user%d", conn.RemoteAddr().(*net.TCPAddr).Port)
	go client.startSend()

	chatState.clientsLock.Lock()
	chatState.clients[conn] = client
	chatState.numClients++
	chatState.clientsLock.Unlock()

	defer func() {
		chatState.clientsLock.Lock()
		delete(chatState.clients, conn)
		chatState.numClients--
		chatState.clientsLock.Unlock()
		conn.Close()
	}()

loop:
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		switch msgType {
		case websocket.TextMessage:
			msg := string(data)
			msg = strings.TrimSpace(msg)
			if len(msg) > 0 && msg[0] == '/' {
				// 处理命令
				parts := strings.SplitN(msg, " ", 2)
				cmd := parts[0]
				if cmd == "/nick" && len(parts) > 1 {
					if len(parts[1]) > 32 {
						client.conn.WriteMessage(msgType, []byte(">> 昵称太长了"))
						continue loop
					}
					client.nick = parts[1]
					client.conn.WriteMessage(msgType, []byte(">> 欢迎 "+client.nick))
				}
				continue loop
			}
			if strings.ToLower(msg) == "quit" {
				return
			}

			chatState.clientsLock.RLock()
			for _, c := range chatState.clients {
				if c == client {
					c.sendChan <- "#>> " + client.nick + ": " + msg
				} else {
					c.sendChan <- ">> " + client.nick + ": " + msg
				}

			}
			chatState.clientsLock.RUnlock()
		case websocket.CloseMessage:
			return
		case websocket.PingMessage:
			conn.WriteMessage(websocket.PongMessage, data)
		case websocket.PongMessage:
		default:
		}

	}

}
