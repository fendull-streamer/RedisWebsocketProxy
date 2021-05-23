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
	"os"
	"sync"
)

type PubSubMessage struct {
	Type int
	Value string
	Channel string
}


type PubSubClientConn struct {
	Conn *websocket.Conn
	Lock sync.Mutex
}

func (c *PubSubClientConn) ReadJSON(v interface{}) (error) {
	
	
	err := c.Conn.ReadJSON(v)
	return err
}

func (c *PubSubClientConn) WriteJSON(v interface{}) (error) {
	
	err := c.Conn.WriteJSON(v)
	return err
}

type PubSubBroker struct {
	Submap map[string]map[*PubSubClientConn]bool
	Chmap map[*PubSubClientConn]map[string]bool
	Upgrader websocket.Upgrader
	Address string
	MessageQueue chan PubSubMessage
}
func (p *PubSubBroker) Start() {
	go func(){
		for msg := range p.MessageQueue {
			clients, ok := p.Submap[msg.Channel]
			if ok {
				for conn, _ := range clients {
					conn.WriteJSON(msg)
				}
			}
		}
	}()
}
func (p *PubSubBroker) addSub(ch string, conn *PubSubClientConn) {
	_, ok := p.Submap[ch]
	if !ok {
		p.Submap[ch] = make(map[*PubSubClientConn]bool)
	} 
	p.Submap[ch][conn] = true

	_, ok = p.Chmap[conn]
	if !ok {
		p.Chmap[conn] = make(map[string]bool)
		
	} 
	p.Chmap[conn][ch] = true
}

func (p *PubSubBroker) removeSub(ch string, conn *PubSubClientConn){
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

func (p *PubSubBroker) removeAllSubs(conn *PubSubClientConn) {
	for ch, _ := range p.Chmap[conn] {
		delete(p.Submap[ch], conn)
	}
	delete(p.Chmap, conn)
}

func (p *PubSubBroker) reader(w http.ResponseWriter, r *http.Request) {
	c, err := p.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}

	pbconn := PubSubClientConn{Conn: c, Lock: sync.Mutex{}}
	
	defer c.Close()
	defer p.removeAllSubs(&pbconn)
	
	for {
		message := PubSubMessage{}
		err := pbconn.ReadJSON(&message)
		if err != nil {
			log.Println("read:", err)
			break
		}

		if message.Type == 1 {
			go func(msg PubSubMessage) {
				p.MessageQueue <- msg
			}(message)
			
		} else if message.Type == 2 {
			p.addSub(message.Channel, &pbconn)
			err = pbconn.WriteJSON(PubSubMessage{Type: 2, Value: "subscribed", Channel: message.Channel})
			if err != nil {
				log.Println("write:", err)
				break
			}
		} else if message.Type == 3 {
			p.removeSub(message.Channel, &pbconn)
			
			err = pbconn.WriteJSON(PubSubMessage{Type: 3, Value: "Success", Channel: message.Channel})
			if err != nil {
				log.Println("write:", err)
				break
			}
		}
		
	}
}

func main() {
	
	proxy := PubSubBroker{
		Submap: make(map[string]map[*PubSubClientConn]bool),
		Chmap: make(map[*PubSubClientConn]map[string]bool),
		Upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {return true}},
		Address: ":" + os.Getenv("REDIS_PROXY_PORT"),
		MessageQueue: make(chan PubSubMessage)}
	
		
	proxy.Start()
	flag.Parse()
	log.SetFlags(0)
	http.HandleFunc("/", proxy.reader)
	log.Fatal(http.ListenAndServe(proxy.Address, nil))
}

