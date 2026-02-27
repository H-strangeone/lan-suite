package transport

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 32 * 1024
)

type Client struct {
	hub         *Hub
	conn        *websocket.Conn
	send        chan *Message
	nodeID      string
	displayName string
	services    []string
	currentRoom string
}

func newClient(hub *Hub, conn *websocket.Conn) *Client {
	return &Client{
		hub:  hub,
		conn: conn,
		send: make(chan *Message, 256),
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
		log.Printf("[ws] disconnected: %s", c.identity())
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(_ string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, rawBytes, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived,
			) {
				log.Printf("[ws] unexpected close %s: %v", c.identity(), err)
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(rawBytes, &msg); err != nil {
			c.sendMsg(NewErrorMsg("parse_error", "message must be valid JSON"))
			continue
		}

		msg.From = c.nodeID

		select {
		case c.hub.incoming <- &incomingMsg{client: c, msg: &msg}:
		default:
			log.Printf("[ws] hub overloaded, dropping from %s", c.identity())
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteJSON(msg); err != nil {
				log.Printf("[ws] write error %s: %v", c.identity(), err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[ws] ping error %s: %v", c.identity(), err)
				return
			}
		}
	}
}

func (c *Client) sendMsg(msg *Message) {
	select {
	case c.send <- msg:
	default:
		log.Printf("[ws] send buffer full for %s — dropping %s", c.identity(), msg.Type)
	}
}

func (c *Client) identity() string {
	name := c.displayName
	if name == "" {
		name = "anon"
	}
	id := c.nodeID
	if len(id) > 8 {
		id = id[:8]
	}
	return name + "/" + id
}

func (c *Client) asPeerInfo() PeerInfo {
	svcs := c.services
	if svcs == nil {
		svcs = []string{}
	}
	return PeerInfo{NodeID: c.nodeID, DisplayName: c.displayName, Services: svcs}
}

func ServeWS(hub *Hub, allowedOrigins map[string]bool) http.HandlerFunc {
	u := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return len(allowedOrigins) == 0
			}
			return allowedOrigins[origin]
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := u.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[ws] upgrade failed from %s: %v", r.RemoteAddr, err)
			return
		}

		client := newClient(hub, conn)
		hub.register <- client
		go client.writePump()
		go client.readPump()
	}
}