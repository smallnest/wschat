package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

var addr = flag.String("addr", ":8080", "http service address")

type Client struct {
	conn     *websocket.Conn
	nick     string
	sendChan chan string
}

func (c Client) startSend() {
	for msg := range c.sendChan {
		c.conn.Write(context.Background(), websocket.MessageText, []byte(msg))
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
	http.ServeFile(w, r, "../home.html")
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

func serveWs(chatState *ChatState, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{conn: conn, sendChan: make(chan string, 256)}
	_, port, _ := net.SplitHostPort(r.RemoteAddr)
	if port == "" {
		port = fmt.Sprintf("%d", rand.Int32())
	}

	client.nick = fmt.Sprintf("user%s", port)
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
		conn.Close(websocket.StatusGoingAway, "")
	}()

loop:
	for {
		msgType, data, err := conn.Read(context.Background())
		if err != nil {
			break
		}

		switch msgType {
		case websocket.MessageText:
			msg := string(data)
			msg = strings.TrimSpace(msg)
			if len(msg) > 0 && msg[0] == '/' {
				// 处理命令
				parts := strings.SplitN(msg, " ", 2)
				cmd := parts[0]
				if cmd == "/nick" && len(parts) > 1 {
					if len(parts[1]) > 32 {
						client.conn.Write(context.Background(), msgType, []byte(">> 昵称太长了"))
						continue loop
					}
					client.nick = parts[1]

					msg = ">> 欢迎 " + client.nick + "加入聊天室\n"

					var users []string
					chatState.clientsLock.RLock()
					for _, c := range chatState.clients {
						users = append(users, c.nick)
					}
					chatState.clientsLock.RUnlock()
					sort.Strings(users)
					msg += ">> 当前聊天室用户: " + strings.Join(users, ", ")

					chatState.clientsLock.RLock()
					for _, c := range chatState.clients {
						c.sendChan <- msg
					}
					chatState.clientsLock.RUnlock()

					continue loop
				}

				if msg == "/list" || msg == "/users" || msg == "/who" {
					var users []string
					chatState.clientsLock.RLock()
					for _, c := range chatState.clients {
						users = append(users, c.nick)
					}
					chatState.clientsLock.RUnlock()
					sort.Strings(users)
					msg = ">> 当前聊天室用户: " + strings.Join(users, ", ")
					client.sendChan <- msg
					continue loop
				}

				if msg == "quit" {
					client.sendChan <- ">> 再见 " + client.nick
					return
				}
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
		default:
		}

	}

}
