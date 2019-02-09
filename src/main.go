package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/rs/cors"
	"github.com/rs/xid"
	"log"
	"net/http"
	"sort"
	"time"
)

//Actually connection to game id
var Lobby map[string]Game

type Game struct {
	Name      string `json:"name"`
	Id        string `json:"id"`
	broadcast chan Message
	Started   bool `json:"started"`
	clients   map[*websocket.Conn]*Client
	time      time.Time
}

type Client struct {
	Username string `json:"username"`
	Ready    bool   `json:"ready"`
	time     time.Time
}

type Message struct {
	Type string
	Msg  json.RawMessage
}

type CreateGameParams struct {
	Name string `json:"name"`
}

var upgrader = websocket.Upgrader{}

func main() {
	Lobby = make(map[string]Game)
	http.HandleFunc("/", handler)
	http.HandleFunc("/createGame", postCreateGame)
	http.HandleFunc("/ws/", wsPage)
	handler := cors.Default().Handler(http.DefaultServeMux)
	http.ListenAndServe(":8000", handler)
}

func postCreateGame(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
	fmt.Println(r.Body)
	var params CreateGameParams
	err := json.NewDecoder(r.Body).Decode(&params)
	g := Game{params.Name,
		xid.New().String(),
		make(chan Message),
		false,
		make(map[*websocket.Conn]*Client),
		time.Now()}
	//We create a user here, but we need a socket to store it.
	g.Name = params.Name
	fmt.Println(g)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	Lobby[g.Id] = g
	w.WriteHeader(200)
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(g.Id)
	w.Write(data)
	go handleMessages(g.Id)
}

func wsPage(rw http.ResponseWriter, req *http.Request) {
	fmt.Printf("Req: %s", req.URL.Path)
	keys, ok := req.URL.Query()["gid"]

	if !ok || len(keys[0]) < 1 {
		log.Println("Url Param 'key' is missing")
		return
	}
	gid := keys[0]
	fmt.Printf(gid)
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	ws, err := upgrader.Upgrade(rw, req, nil)
	//Create a client and the add them to their game
	//Lobby[keys[0]].broadcast <- Message{"HEY"}
	if err != nil {
		log.Fatal(err)
	}
	// Make sure we close the connection when the function returns
	defer ws.Close()

	//Read in messages from the websocket
	for {
		var msg Message
		// Read in a new message as JSON and map it to a Message object
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("error: %v", err)
			delete(Lobby[gid].clients, ws)
			if len(Lobby[gid].clients) < 1 {
				delete(Lobby, gid)
			} else {
				err = Lobby[gid].sendReady()
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
				}
			}
			break
		}
		if msg.Type == "join" {
			client := Client{"", false, time.Now()}
			err = json.Unmarshal(msg.Msg, &client)
			fmt.Println(client.Username)
			Lobby[gid].clients[ws] = &client
			err = Lobby[gid].sendReady()
			if err != nil {
				http.Error(rw, err.Error(), http.StatusInternalServerError)
			}
		} else if msg.Type == "ready" {
			if !Lobby[gid].Started {
				Lobby[gid].clients[ws].Ready = !Lobby[gid].clients[ws].Ready
				err = Lobby[gid].sendReady()
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
				}
			}
		} else if msg.Type == "action" {

		}
	}
}

func (g Game) sendReady() error {
	m := make([]*Client, 0, len(g.clients))
	for _, val := range g.clients {
		m = append(m, val)
	}
	sort.Slice(m, func(i, j int) bool { return m[i].time.Before(m[j].time) })
	fmt.Println(m)
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	g.broadcast <- Message{"ready", data}
	return nil
}

func handler(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		m := make([]Game, 0, len(Lobby))
		for _, val := range Lobby {
			m = append(m, val)
		}
		sort.Slice(m, func(i, j int) bool { return !m[i].time.Before(m[j].time) })
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

func handleMessages(gid string) {
	for {
		// Grab the next message from the broadcast channel
		msg := <-Lobby[gid].broadcast
		// Send it out to every client that is currently connected
		for client := range Lobby[gid].clients {
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("error: %v", err)
			}
		}
	}

	return
}
