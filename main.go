package main

import (
    "log"
    "sort"
    "strings"
	"net/http"
    "io/ioutil"
    "math/rand"
    "bytes"
    "html/template"
    "github.com/gorilla/websocket"
)

var room_lookup map[string]*Room

func generate_rune() rune {
    ind := rand.Intn(36)
    if ind < 26 {
        return rune('A' + ind)
    }
    return rune(ind - 26 + '0')
}

func generate_room_code() string {
    var sb strings.Builder
    code_length := 4
    for i := 0; i < code_length; i++ {
        sb.WriteRune(generate_rune())
    }
    return sb.String()
}

func store_room(r *Room) string {
    for {
        c := generate_room_code()
        if _, ok := room_lookup[c]; ok {
            // already exists
            continue
        }
        room_lookup[c] = r;
        return c;
    }
}

type event struct {
    Name string `json:"event"`
    Data interface{} `json:"data"`
}

type Record struct {
    Player *Player
    Words []string
    Score int
}

type Game struct {
    Records []*Record
    Phrase string
    TimeLeft int
    Room *Room
}

type Room struct {
    Players []*Player
    Code string
    Game *Game
}

func create_room() *Room {
    r := &Room{}
    r.Players = make([]*Player, 0)
    r.Code = store_room(r)
    return r
}

func applyTemplate(path string, data interface{}) string {
    var out bytes.Buffer
    tmpl := template.Must(template.ParseFiles(path))
    err := tmpl.Execute(&out, data)
    if err != nil {
        log.Printf("Error templating %v when templating %v", err, path)
    }
    return out.String()
}

func (r *Room) to_page_event() event {
    e := event{}
    e.Name = "show_page"
    e.Data = applyTemplate("static/pages/room.html", r)
    return e
}

func (r *Room) broadcast(e *event) {
    for _,p := range r.Players {
        p.Conn.WriteJSON(e)
    }
}

func (r *Room) update_room() {
    e := r.to_page_event()
    r.broadcast(&e)
}

func generate_phrase() string {
    return "the cat in the hat is back"
}

func create_game() *Game {
    g := &Game{}
    g.Phrase = generate_phrase()
    g.Records = make([]*Record,0)
    return g
}

func (g *Game) fill_game(r *Room) {
    for _,p := range r.Players {
        rec := &Record{}
        rec.Player = p
        rec.Words = make([]string,0)
        rec.Score = 0
        g.Records = append(g.Records, rec)
    }
}

type GameUpdate struct {
    SecondsLeft int
    RecordInfo string
}

func (g *Game) create_game_update() *GameUpdate {
    gu := &GameUpdate{}
    gu.SecondsLeft = g.TimeLeft
    gu.RecordInfo = applyTemplate("static/pages/game_records.html", g)
    return gu
}

func (g *Game) update_game() {
    e := &event{}
    e.Name = "game_update"
    e.Data = g.create_game_update()
    g.Room.broadcast(e)
}

func (g *Game) word_is_substring(word string) bool {
    return true;
}

func (g *Game) word_pts(word string) int {
    return 100;
}

func (g *Game) submit_word(word string, p *Player) {
    if (g.word_is_substring(word)) {
        // find record
        ind := -1
        for i,rec := range g.Records {
            if rec.Player == p {
                ind = i
                break
            }
        }
        g.Records[ind].Score += g.word_pts(word)
        g.Records[ind].Words = append(g.Records[ind].Words, word)
        sort.Slice(g.Records, func(i,j int) bool {
            return g.Records[i].Score > g.Records[j].Score
        })
        g.update_game()
    }
}

func (g *Game) to_page_event() *event {
    e := &event{}
    e.Name = "show_page"
    e.Data = applyTemplate("static/pages/game.html", g)
    return e
}

func (r *Room) setup_game() {
    g := create_game()
    g.fill_game(r)
    r.Game = g
    g.Room = r
    g.TimeLeft = 15
    e := g.to_page_event()
    r.broadcast(e)
    g.update_game()
}

func (r *Room) add_player(p *Player) {
    r.Players = append(r.Players, p)
    p.Room = r
    r.update_room()
}

func (r *Room) remove_player(p *Player) {
    to_del := -1
    arr := r.Players
    for i,p2 := range arr {
        if p2 == p {
            to_del = i
        }
    }
    if to_del != -1 {
        arr[to_del] = arr[len(arr)-1]
        arr = arr[:len(arr)-1]
        r.Players = arr
    }

    // remove room if empty
    if len(arr) == 0 {
        delete(room_lookup, r.Code)
        log.Printf("Removed room %v", r.Code)
    }
    p.Room = nil
    r.update_room()
}



type Player struct {
    Name string
    Conn *websocket.Conn
    Room *Room
    Score int
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}


func read_page(name string) string {
    bytes, err := ioutil.ReadFile("static/pages/" + name)
	if err != nil {
        log.Printf("Could not find file %v: ", name);
	}

    return string(bytes);
}

func serve_socket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
    log.Println("Opened a websocket to user.")

    p := &Player{}
    p.Name = "user"
    p.Conn = conn
    p.Score = 0

    for {
        e := event{}
        err := conn.ReadJSON(&e)

		if err != nil {
            if p.Room != nil {
                p.Room.remove_player(p)
            }
            log.Printf("unexpected error: %v", err)
			break
		}

        log.Printf("Recieved %v event", e.Name)
        switch e.Name {
        case "GET_home":
            // remove from room if already there
            if p.Room != nil {
                p.Room.remove_player(p)
            }

            e2 := event{}
            e2.Name = "show_page"
            e2.Data = read_page("home.html")
            conn.WriteJSON(e2)
        case "start_game":
            if p.Room == nil {
                log.Println("Illegal action, booting player %v", p.Name)
                break
            }
            p.Room.setup_game()
        case "submit_word":
            if (p.Room == nil || p.Room.Game == nil) {
                log.Println("No game running, word submission ignored");
            }
            p.Room.Game.submit_word(e.Data.(string), p)
        case "GET_room":
            r := create_room()
            r.add_player(p)
        case "GET_join_room":
            e2 := event{}
            e2.Name = "show_page"
            e2.Data = read_page("join_room.html")
            conn.WriteJSON(e2)
        case "submit_room_code":
            code := e.Data.(string)
            r, ok := room_lookup[code]
            if ok {
                r.add_player(p)
                e2 := r.to_page_event()
                conn.WriteJSON(e2)
            } else {
                e2 := event{}
                e2.Name = "room_not_found"
                conn.WriteJSON(e2)
            }
        case "set_name":
            p.Name = e.Data.(string)
        default:
            log.Println("Unsupported event type")
        }
        log.Printf("Sent response for %v event ", e.Name)

    }
}

func main() {
    // init globals
    room_lookup = make(map[string]*Room)

    // serve static files
    fs := http.FileServer(http.Dir("static"))
    http.Handle("/", fs)

    // accept websocket connections
	http.HandleFunc("/ws", serve_socket)

    err := http.ListenAndServe("localhost:8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
