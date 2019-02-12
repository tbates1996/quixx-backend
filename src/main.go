package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/rs/cors"
	"github.com/rs/xid"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"time"
)

/*
 * Domain model
 */
var Lobby map[string]*Game

var top = []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
var bottom = []int{12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2}

type Game struct {
	Name        string `json:"name"`
	Id          string `json:"id"`
	broadcast   chan Message
	Started     bool `json:"started"`
	Finished    bool `json:"finished"`
	TurnStarted bool `json:"turnStarted"`
	clients     map[*websocket.Conn]*Client
	Die         *Die `json:"die"`
	time        time.Time
}

type GameMessage struct {
	Name     string    `json:"name"`
	Started  bool      `json:"started"`
	Finished bool      `json:"finished"`
	Clients  []*Client `json:"clients"`
	Die      *Die      `json:"die"`
}

type Client struct {
	Username    string `json:"username"`
	Ready       bool   `json:"ready"`
	CurrentTurn bool   `json:"currentTurn"`
	Board       *Board `json:"board"`
	Score       int    `json:"score"`
	time        time.Time
}

type Message struct {
	Type string          `json:"type"`
	Msg  json.RawMessage `json:"msg"`
}

type CreateGameParams struct {
	Name string `json:"name"`
}

type Die struct {
	Red        int  `json:"red"`
	Blue       int  `json:"blue"`
	Green      int  `json:"green"`
	Yellow     int  `json:"yellow"`
	White1     int  `json:"white1"`
	White2     int  `json:"white2"`
	BlueLock   bool `json:"blueLock"`
	RedLock    bool `json:"redLock"`
	GreenLock  bool `json:"greenLock"`
	YellowLock bool `json:"yellowLock"`
}
type Board struct {
	Rows    [][]int `json:"rows"`
	actions []Action
}

type Action struct {
	Row     int `json:"row"`
	Col     int `json:"col"`
	peoples bool
	color   bool
}

var upgrader = websocket.Upgrader{}

func main() {
	Lobby = make(map[string]*Game)
	http.HandleFunc("/", handler)
	http.HandleFunc("/createGame", postCreateGame)
	http.HandleFunc("/ws/", wsPage)
	handler := cors.Default().Handler(http.DefaultServeMux)
	http.ListenAndServe(":8000", handler)
}

/*
 * Web handlers
 */
func postCreateGame(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
	fmt.Println(r.Body)
	var params CreateGameParams
	err := json.NewDecoder(r.Body).Decode(&params)
	g := &Game{params.Name,
		xid.New().String(),
		make(chan Message),
		false,
		false,
		false,
		make(map[*websocket.Conn]*Client),
		&Die{1, 1, 1, 1, 1, 1, false, false, false, false},
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
			if Lobby[gid].Started && len(Lobby[gid].clients) == 1 {
				Lobby[gid].Finished = true
			}
			if len(Lobby[gid].clients) < 1 {
				delete(Lobby, gid)
			} else {
				err = Lobby[gid].sendGame()
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
				}
			}
			break
		}
		if msg.Type == "join" {
			client := Client{"", false, false, initBoard(), 0, time.Now()}
			err = json.Unmarshal(msg.Msg, &client)
			fmt.Println(client.Username)
			if len(Lobby[gid].clients) < 5 {
				Lobby[gid].clients[ws] = &client
			} else {
				ws.WriteJSON(Message{"error", nil})
			}
		} else if msg.Type == "ready" {
			//Only time we dont want to ready up is if game is started and we are waiting for a roll aka turn started
			// We also dont want to let the current player ready if no moves have been made
			if !(Lobby[gid].Started && !Lobby[gid].TurnStarted) &&
				!(Lobby[gid].clients[ws].CurrentTurn && len(Lobby[gid].clients[ws].Board.actions) == 0) {
				Lobby[gid].clients[ws].Ready = !Lobby[gid].clients[ws].Ready
			}
			Lobby[gid].updateStatus()
		} else if msg.Type == "roll" {
			if !Lobby[gid].TurnStarted && Lobby[gid].clients[ws].CurrentTurn {
				game := Lobby[gid]
				game.Die = &Die{rand.Intn(6) + 1, rand.Intn(6) + 1, rand.Intn(6) + 1,
					rand.Intn(6) + 1, rand.Intn(6) + 1, rand.Intn(6) + 1, game.Die.BlueLock, game.Die.RedLock,
					game.Die.GreenLock, game.Die.YellowLock}
				game.TurnStarted = true
				Lobby[gid] = game
			}
		} else if msg.Type == "action" {
			var action Action
			err = json.Unmarshal(msg.Msg, &action)
			fmt.Println("Action recieved: ", action)
			//In order for a move to be made the game must be started but not finished and the
			//client must not be ready
			if Lobby[gid].Started && !Lobby[gid].Finished && !Lobby[gid].clients[ws].Ready {
				Lobby[gid].clients[ws].tryAction(action, gid)
			}
		}
		err = Lobby[gid].sendGame()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}

	}
}

func handler(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		m := make([]*Game, 0, len(Lobby))
		for _, val := range Lobby {
			if !val.Started && len(val.clients) < 5 {
				m = append(m, val)
			}
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

/*
 * Game functions
 */
func (g Game) sendGame() error {
	m := make([]*Client, 0, len(g.clients))
	for _, val := range g.clients {
		m = append(m, val)
	}
	sort.Slice(m, func(i, j int) bool { return m[i].time.Before(m[j].time) })
	fmt.Println(m)
	gameMessage := GameMessage{g.Name, g.Started, g.Finished, m, g.Die}
	fmt.Println(gameMessage)
	data, err := json.Marshal(gameMessage)
	if err != nil {
		return err
	}
	g.broadcast <- Message{"waiting", data}
	return nil
}

func (g *Game) updateStatus() {
	//Check to see if everyone is ready
	ready := true
	reset := false
	for _, client := range g.clients {
		ready = ready && client.Ready
	}

	//If so then check to see if the game is started or not.
	if g.Started && ready {
		//If it is started then solidify all moves and then check game status
		g.TurnStarted = false
		reset = true

		// finalize moves and then check for end game status
		g.finalizeMoves()
		//If finished set finish flag
		isGameFinished := g.checkEndState()
		if isGameFinished {
			g.Finished = true
			g.score()
		} else {
			//If not turn is not started and set the current player
			g.setNextPlayer()
		}
	} else if ready {
		// Turn the started flag on and then set the starting player if there are at least two players.
		if len(g.clients) > 1 {
			g.Started = true
			reset = true
			g.setStartingPlayer()
		}

	}
	if reset {
		for _, client := range g.clients {
			client.Ready = false
		}
	}
}

func (g *Game) setNextPlayer() {
	m := make([]*Client, 0, len(g.clients))
	for _, val := range g.clients {
		m = append(m, val)
	}
	sort.Slice(m, func(i, j int) bool { return m[i].time.Before(m[j].time) })
	found := false
	for _, val := range m {
		if found == true {
			val.CurrentTurn = true
			found = false
		} else if val.CurrentTurn == true {
			found = true
			val.CurrentTurn = false
		}
	}
	if found {
		m[0].CurrentTurn = true
	}

}
func (g *Game) setStartingPlayer() {
	m := make([]*Client, 0, len(g.clients))
	for _, val := range g.clients {
		m = append(m, val)
	}
	sort.Slice(m, func(i, j int) bool { return m[i].time.Before(m[j].time) })
	m[0].CurrentTurn = true
}

func (g *Game) finalizeMoves() {
	for _, val := range g.clients {
		for _, action := range val.Board.actions {
			if action.Col == 11 {
				g.lockDie(action)
			}
		}
		val.Board.actions = []Action{}
	}
}

func (g *Game) lockDie(action Action) {
	if action.Row == 0 {
		g.Die.RedLock = true
	} else if action.Row == 1 {
		g.Die.YellowLock = true
	} else if action.Row == 2 {
		g.Die.GreenLock = true
	} else if action.Row == 3 {
		g.Die.BlueLock = true
	}
}

func (g *Game) checkEndState() bool {
	i := 0
	if g.Die.RedLock {
		i++
	}
	if g.Die.YellowLock {
		i++
	}
	if g.Die.GreenLock {
		i++
	}
	if g.Die.GreenLock {
		i++
	}
	if i >= 2 {
		return true
	}
	for _, client := range g.clients {
		i = 0
		for j := 0; j < 4; j++ {
			i += client.Board.Rows[4][j]
		}
		if i == 4 {
			return true
		}
	}
	return false
}

func (g *Game) score() {
	for _, client := range g.clients {
		client.Score = client.Board.score()
	}
}

/*
 * Board Functions
 */
func initBoard() *Board {
	rows := [][]int{[]int{}, []int{}, []int{}, []int{}, []int{}}

	for i := 0; i < 12; i++ {
		for j := 0; j < 4; j++ {
			rows[j] = append(rows[j], 0)
		}
	}
	for j := 0; j < 4; j++ {
		rows[4] = append(rows[4], 0)
	}
	fmt.Println(rows)
	return &Board{rows, []Action{}}
}

func (b *Board) score() int {
	score := 0
	for i := 0; i < 4; i++ {
		rowCount := 0
		for j := 0; j < 12; j++ {
			if b.Rows[i][j] == 1 {
				rowCount++
			}
		}
		score += firstNSum(rowCount)
	}
	skipCount := 0
	for i := 0; i < 4; i++ {
		if b.Rows[4][i] == 1 {
			skipCount++
		}
	}
	return score - 5*skipCount
}

func firstNSum(n int) int {
	sum := 0
	for i := 1; i <= n; i++ {
		sum += i
	}
	return sum
}
func (a Action) equals(other Action) bool {
	return (a.Row == other.Row) && (a.Col == other.Col)
}

func (c *Client) tryAction(action Action, gid string) {
	if action.Row == 4 {
		//This is the skip case we must check to make sure no move has been made and then make skip
		//Check to see if current player and they are not ready
		if c.CurrentTurn && !c.Ready {
			if len(c.Board.actions) > 0 {
				//delete any actions if there exists a skip
				i := 0
				for _, other := range c.Board.actions {
					if !action.equals(other) {
						c.Board.actions[i] = other
						i++
					}
				}
				c.Board.actions = c.Board.actions[:i]
				c.Board.Rows[action.Row][action.Col] = 0
				return

			} else {
				//Skip can be taken
				fmt.Println("Checking to see if skip can be taken")
				for i := 0; i < action.Col; i++ {
					if c.Board.Rows[action.Row][i] == 0 {
						return
					}
				}
				// Okay we are at the slot but we also want to make sure
				//that it is not already taken too
				if c.Board.Rows[action.Row][action.Col] != 1 {
					fmt.Println("Okay we are appending now")
					c.Board.Rows[action.Row][action.Col] = 1
					c.Board.actions = append(c.Board.actions, action)
				}
			}
		}
	} else {
		//Check to see if insert or delete
		insert := c.Board.isInsert(action)
		if insert {
			fmt.Println("We are attempting to insert")
			//If we are inserting then we must make sure len actions is 1 or 2 if its a lock
			//Check to see if we are locking if so just try to do it since it doesnt depend on roll
			if action.Col == 11 {
				fmt.Println("We are attempting to lock")
				c.Board.tryLock(action, gid)
			} else {
				// Are we trying to lock the rightmost value? Check to see if we have
				//5 in that row yet
				if action.Col == 10 {
					validRightmost := c.Board.validRightmost(action)
					if !validRightmost {
						return
					}
				}
				// Now we check to see if the row is locked
				isLocked := isLocked(action, Lobby[gid].Die)
				if isLocked {
					return
				}
				//This is a non lock opperation. We assume someone can lock at any time
				fmt.Println("We are attempting a non locking opperation")
				numNonLock := c.Board.numNonLock()
				// Evaluate the move to set the flags
				evaluateMove(&action, gid)
				fmt.Println("New action flags, ", action)
				if numNonLock == 0 {
					fmt.Println("The number of non lock actions is 0")
					//Check to make sure we should try
					if action.peoples || (action.color && c.CurrentTurn) {
						c.Board.tryRegularInsert(action, gid)
					}
				} else if numNonLock == 1 && c.CurrentTurn {
					fmt.Println("The number of non lock actions is 1 and this is the current user")
					//Check to see if we are inserting to the same row and if it is valid
					sameRow := c.Board.isSameRow(action)
					if sameRow {
						if c.Board.isValidSameRow(action) {
							c.Board.trySameRowInsert(action, gid)
						}
					} else {
						//Get the other action and make sure that they are different types
						otherAction := c.Board.getOtherAction()
						if (otherAction.color && action.peoples) || (otherAction.peoples && action.color) {
							c.Board.tryRegularInsert(action, gid)
						}
					}
				}
			}
		} else {
			//We are deleting here and we want to make sure we dont delete same row
			isValid := c.Board.isValidDelete(action)
			if isValid {
				//Check to see if the delete is the rightmost action
				fmt.Println("We delete here ")
				i := 0
				for _, other := range c.Board.actions {
					if !action.equals(other) {
						c.Board.actions[i] = other
						i++
					}
				}
				c.Board.actions = c.Board.actions[:i]
				c.Board.Rows[action.Row][action.Col] = 0
				return
			}
		}
	}
}

func (b *Board) isInsert(action Action) bool {
	for _, other := range b.actions {
		if action.equals(other) {
			return false
		}
	}
	return true
}

func (b *Board) isSameRow(action Action) bool {
	for _, other := range b.actions {
		if action.Row == other.Row && other.Col != 11 {
			return true
		}
	}
	return false
}

func (b *Board) isValidSameRow(action Action) bool {
	otherAction := b.getOtherAction()
	if otherAction.Col > action.Col {
		return otherAction.color && action.peoples
	} else if otherAction.Col < action.Col {
		return otherAction.peoples && action.color
	} else {
		return false
	}
}

func (b *Board) isValidDelete(action Action) bool {
	if action.Col == 10 {
		for _, other := range b.actions {
			if action.Row == other.Row && other.Col == 11 {
				return false
			}
		}
	}
	return true
}

//Attempts to lock out the row. If the farmost right is locked then
// we lock the row and wait for moves to be finalized to lock the die.
func (b *Board) tryLock(action Action, gid string) {
	if b.Rows[action.Row][10] == 1 {
		b.actions = append(b.actions, action)
		b.Rows[action.Row][action.Col] = 1
	}
}

func (b *Board) getOtherAction() Action {
	var action Action
	for _, val := range b.actions {
		if val.Col != 11 {
			action = val
		}
	}
	return action
}

//Get the number of non lock actions that the user has taken
func (b *Board) numNonLock() int {
	i := 0
	for _, val := range b.actions {
		if val.Col != 11 {
			i++
		}
	}
	return i
}

func (b *Board) tryRegularInsert(action Action, gid string) {
	/*
	 * Okay so we need to make sure the row isnt locked out to begin with
	 * if it is then we dont even attempt to insert the action. from there
	 * we will go on to make sure that the action is the furthest forward X
	 * in the current row of their board. If so we make the move and add the
	 * action to the list. If not do nothing.
	 */
	for i := len(b.Rows[action.Row]) - 1; i >= action.Col; i-- {
		if b.Rows[action.Row][i] == 1 {
			return
		}
	}
	b.actions = append(b.actions, action)
	b.Rows[action.Row][action.Col] = 1
}

func (b *Board) validRightmost(action Action) bool {
	i := 0
	for j := 0; j < 10; j++ {
		if b.Rows[action.Row][j] == 1 {
			i++
		}
	}
	return i >= 5
}

func (b *Board) trySameRowInsert(action Action, gid string) {
	/*
	 *Okay we need to get the other action and then from there we
	 * need ot need to see if its before of after our current insert.
	 * if it is before then we can add no question, but if it is after
	 * we need to make sure that there is no 1 between otherAction.Col
	 * and action.Col
	 */
	otherAction := b.getOtherAction()
	if otherAction.Col > action.Col {
		//Tricky case
		for i := otherAction.Col - 1; i >= action.Col; i-- {
			if b.Rows[action.Row][i] == 1 {
				return
			}
		}
		b.actions = append(b.actions, action)
		b.Rows[action.Row][action.Col] = 1
	} else {
		b.actions = append(b.actions, action)
		b.Rows[action.Row][action.Col] = 1
	}
}

func isLocked(action Action, die *Die) bool {
	if action.Row == 0 {
		return die.RedLock
	} else if action.Row == 1 {
		return die.YellowLock
	} else if action.Row == 2 {
		return die.GreenLock
	} else if action.Row == 3 {
		return die.BlueLock
	}
	return true
}

//Set the flags on the action obect
func evaluateMove(action *Action, gid string) {
	number := 0
	if action.Row < 2 {
		number = top[action.Col]
	} else if action.Row >= 2 {
		number = bottom[action.Col]
	}

	die := Lobby[gid].Die
	if number == die.White1+die.White2 {
		action.peoples = true
	}
	if action.Row == 0 {
		if die.White1+die.Red == number || die.White2+die.Red == number {
			action.color = true
		}
	} else if action.Row == 1 {
		if die.White1+die.Yellow == number || die.White2+die.Yellow == number {
			action.color = true
		}
	} else if action.Row == 2 {
		if die.White1+die.Green == number || die.White2+die.Green == number {
			action.color = true
		}
	} else if action.Row == 3 {
		if die.White1+die.Blue == number || die.White2+die.Blue == number {
			action.color = true
		}
	}
}
