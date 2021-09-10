package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/posener/auth"
	"golang.org/x/oauth2"
)

var (
	addr       = flag.String("addr", ":8080", "Address to listen to")
	configPath = flag.String("config", "config.json", "Config file to load.")
)

//go:embed index.html script.js style.css exams.json exams/*
var static embed.FS

var serveStatic = http.FileServer(http.FS(static))

//go:embed admin.html.gotmpl
var adminPage []byte

var adminTmpl = template.Must(template.New("admin.html").Parse(string(adminPage)))

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	flag.Parse()

	for {
		ctx, cancel = context.WithCancel(ctx)
		again := false
		reset := func() {
			again = true
			cancel()
		}

		h, err := newHandler(reset)
		if err != nil {
			log.Fatalf("Failed loading handler: %v", err)
		}

		a, err := auth.New(ctx, auth.Config{
			Config: oauth2.Config{
				RedirectURL:  fmt.Sprintf("%s/auth", h.config.Address),
				ClientID:     h.config.ClientID,
				ClientSecret: h.config.ClientSecret,
			},
		})
		if err != nil {
			log.Fatalf("Failed setting up auth: %v", err)
		}

		mux := http.NewServeMux()
		mux.Handle("/", a.Authenticate(http.HandlerFunc(h.serveStatic)))
		mux.Handle("/admin", a.Authenticate(http.HandlerFunc(h.serveAdmin)))
		mux.Handle("/auth", a.RedirectHandler())

		srv := http.Server{
			Addr:    *addr,
			Handler: mux,
		}

		go func() {
			log.Printf("Serving on %s", h.config.Address)
			srv.ListenAndServe()
		}()

		// Wait for context to finish and shutdown the server.
		<-ctx.Done()
		log.Printf("Shutting down server...")
		ctx, cancel = context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		err = srv.Shutdown(ctx)
		if err != nil {
			log.Fatalf("Failed server shut down: %v", err)
		}
		if !again {
			return
		}
	}
}

type handler struct {
	config struct {
		Members      []string `json:"members"`
		Admins       []string `json:"admins"`
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		Address      string   `json:"address"`
	}
	admins  map[string]bool
	members map[string]bool
	reset   func()
}

func newHandler(reset func()) (*handler, error) {
	h := handler{
		reset:   reset,
		admins:  make(map[string]bool),
		members: make(map[string]bool),
	}
	f, err := os.Open(*configPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	err = json.NewDecoder(f).Decode(&h.config)
	if err != nil {
		return nil, err
	}

	for _, v := range h.config.Admins {
		h.admins[v] = true
	}
	for _, v := range h.config.Members {
		h.members[v] = true
	}
	return &h, nil
}

func (h handler) serveStatic(w http.ResponseWriter, r *http.Request) {
	user := auth.User(r.Context())
	if !h.members[user.Email] {
		http.Error(w, fmt.Sprintf("User %s not allowed", user.Email), http.StatusForbidden)
		return
	}
	serveStatic.ServeHTTP(w, r)
}

func (h handler) serveAdmin(w http.ResponseWriter, r *http.Request) {
	user := auth.User(r.Context())
	if !h.admins[user.Email] {
		http.Error(w, fmt.Sprintf("User %s not an admin", user.Email), http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPost:
		defer http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		err := r.ParseForm()
		if err != nil {
			log.Printf("Failed parsing form: %s", err)
			return
		}

		// Check reset mode.
		switch m := mode(r.Form); m {
		case "reset":
			log.Println("Requested server reset...")
			h.reset()
		case "update":
			log.Println("Requested config update...")

			data := r.Form.Get("data")
			var v interface{}
			err := json.Unmarshal([]byte(data), &v)
			if err != nil {
				log.Printf("Failed unmarshaling %s: %s", data, err)
				http.Error(w, fmt.Sprintf("Invalid json data: %s", err), http.StatusBadRequest)
				return
			}
			formattedData, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				log.Printf("Failed marshaling data %+v: %s", v, err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}

			log.Printf("Preparing backup...")
			err = backup(*configPath)
			if err != nil {
				log.Printf("Failed preparing backup: %s", err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}

			log.Printf("Writing new config: \n\n %s\n\n", string(data))
			err = os.WriteFile(*configPath, formattedData, 0)
			if err != nil {
				log.Printf("Failed writing config %s: %s", *configPath, err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
			log.Println("Resetting server...")
			h.reset()
		default:
			log.Printf("Admin got unknown mode: %s", m)
		}
	case http.MethodGet:
		formattedData, err := json.MarshalIndent(h.config, "", "  ")
		if err != nil {
			log.Printf("Failed marshaling data %+v: %s", h.config, err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		err = adminTmpl.Execute(w, string(formattedData))
		if err != nil {
			log.Printf("Failed executing template: %s", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	}
}

func mode(v url.Values) string {
	if len(v["mode"]) == 0 {
		return ""
	}
	return v["mode"][0]
}

func backup(path string) error {
	backupPath := path + ".bck"
	dst, err := os.Create(backupPath)
	if err != nil {
		log.Printf("Failed creating backup file: %s", err)
	}
	defer dst.Close()

	src, err := os.Open(path)
	if err != nil {
		log.Printf("Failed creating backup file: %s", err)
	}
	defer src.Close()

	_, err = io.Copy(dst, src)
	return err
}
