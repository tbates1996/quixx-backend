package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/rs/cors"
	"github.com/rs/xid"
	"log"
	"net/http"
)

//Actually connection to game id
var Lobby map[xid.ID]Game

type Game struct {
	Name      string `json:"name"`
	Id        xid.ID `json:"id"`
	broadcast chan Message
}

type Message struct {
	Type string
}

var upgrader = websocket.Upgrader{}

func main() {
	Lobby = make(map[xid.ID]Game)
	http.HandleFunc("/", handler)
	http.HandleFunc("/createGame", postCreateGame)
	http.HandleFunc("/ws", wsPage)
	handler := cors.Default().Handler(http.DefaultServeMux)
	go handleMessages()
	http.ListenAndServe(":8000", handler)
}

func postCreateGame(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
	fmt.Println(r.Body)
	var g Game
	g.Id = xid.New()
	g.broadcast = make(chan Message)
	err := json.NewDecoder(r.Body).Decode(&g)
	fmt.Println(g)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	Lobby[g.Id] = g
	w.WriteHeader(200)
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(g.Id.String())
	w.Write(data)
}

func wsPage(rw http.ResponseWriter, req *http.Request) {
	fmt.Printf("Req: %s", req.URL.Path)
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	ws, err := upgrader.Upgrade(rw, req, nil)
	ws.WriteJSON("HEY")
	if err != nil {
		log.Fatal(err)
	}
	// Make sure we close the connection when the function returns
	defer ws.Close()
	//Do more shit down here
	for {
		var msg Message
		// Read in a new message as JSON and map it to a Message object
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("error: %v", err)
			break
		}
	}
}

func handler(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		m := make([]Game, 0, len(Lobby))
		for _, val := range Lobby {
			m = append(m, val)
		}
		responseBody := m
		data, err := json.Marshal(responseBody)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		rw.WriteHeader(200)
		rw.Header().Set("Content-Type", "application/json")
		rw.Write(data)
	}
}

func handleMessages() {
	/*for {
		// Grab the next message from the broadcast channel
		msg := <-broadcast
		// Send it out to every client that is currently connected
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("error: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
	*/
	return
}
