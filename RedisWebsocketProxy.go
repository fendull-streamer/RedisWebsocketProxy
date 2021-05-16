// Copyright 2015 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package main



import (
	"flag"
	"log"
	"net/http"
	"github.com/gorilla/websocket"
	"github.com/go-redis/redis/v8"
	"time"
	"os"
)

type ProxyMessage struct {
	Type int
	Value string
	Channel string
}

type PubSubProxy struct {
	RedisDB *redis.Client
	Submap map[string]map[*websocket.Conn]bool
	Chmap map[*websocket.Conn]map[string]bool
	Upgrader websocket.Upgrader
	Address string
}

func (p *PubSubProxy) addSub(ch string, conn *websocket.Conn) {
	_, ok := p.Submap[ch]
	if !ok {
		p.Submap[ch] = make(map[*websocket.Conn]bool)
	} 
	p.Submap[ch][conn] = true

	_, ok = p.Chmap[conn]
	if !ok {
		p.Chmap[conn] = make(map[string]bool)
		
	} 
	p.Chmap[conn][ch] = true
}

func (p *PubSubProxy) removeSub(ch string, conn *websocket.Conn){
	_, ok := p.Submap[ch]
	if ok {
		delete(p.Submap[ch], conn)
		
	} else {
		return
	}

	_, ok = p.Chmap[conn]
	if ok {
		delete(p.Chmap[conn], ch)
	} 
}

func (p *PubSubProxy) removeAllSubs(conn *websocket.Conn) {
	for ch, _ := range p.Chmap[conn] {
		delete(p.Submap[ch], conn)
	}
	delete(p.Chmap, conn)
}

func (p *PubSubProxy) reader(w http.ResponseWriter, r *http.Request) {
	c, err := p.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer p.removeAllSubs(c)
	defer c.Close()
	for {
		message := ProxyMessage{}
		err := c.ReadJSON(&message)
		if err != nil {
			log.Println("read:", err)
			break
		}

		if message.Type == 1 {
			p.RedisDB.Set(p.RedisDB.Context(), message.Channel, message.Value, 0)
			p.RedisDB.Publish(p.RedisDB.Context(), message.Channel, message.Value)
			
		} else if message.Type == 2 {
			p.addSub(message.Channel, c)
			cmd := p.RedisDB.Get(p.RedisDB.Context(), message.Channel)
			val, invalid := cmd.Result()
			if !invalid {
				err = c.WriteJSON(ProxyMessage{Type: 2, Value: val, Channel: message.Channel})
				if err != nil {
					log.Println("write:", err)
					break
				}
			}
		} else if message.Type == 3 {
			p.removeSub(message.Channel, c)
			err = c.WriteJSON(ProxyMessage{Type: 3, Value: "Success", Channel: message.Channel})
			if err != nil {
				log.Println("write:", err)
				break
			}
		}
		
	}
}

func main() {
	rdb := redis.NewClient(&redis.Options{
		Addr: "device.fendull.com:6379",
		Password: "",
		DB: 0,
		DialTimeout:  10 * time.Second,
    	ReadTimeout:  30 * time.Second,
    	WriteTimeout: 30 * time.Second,
    	PoolSize:     10,
    	PoolTimeout:  30 * time.Second,
		})
	rdb.Ping(rdb.Context()).Result()
	pubsub := rdb.PSubscribe(rdb.Context(), "*")
	proxy := PubSubProxy{
		RedisDB: rdb,
		Submap: make(map[string]map[*websocket.Conn]bool),
		Chmap: make(map[*websocket.Conn]map[string]bool),
		Upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {return true}},
		Address: ":" + os.Getenv("REDIS_PROXY_PORT")}
	

	if _, err := pubsub.Receive(rdb.Context()); err != nil {
        return
    }
	ch := pubsub.Channel()
	defer pubsub.Close()
	go func(rdb *redis.Client) {
		for {
			rdb.Publish(rdb.Context(), "connections", "goserver")
			time.Sleep(900*time.Millisecond)
		}
		
	}(rdb)
	go func(ch <-chan *redis.Message) {
		for msg := range ch {
			clients, ok := proxy.Submap[msg.Channel]
			if ok {
				for conn, _ := range clients {
					conn.WriteJSON(ProxyMessage{Type: 1, Value: msg.Payload, Channel: msg.Channel})
				}
			}
		}
	}(ch)
	
	flag.Parse()
	log.SetFlags(0)
	http.HandleFunc("/", proxy.reader)
	log.Fatal(http.ListenAndServe(proxy.Address, nil))
}

