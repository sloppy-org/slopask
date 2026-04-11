package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/sloppy-org/slopask/internal/server"
	"github.com/sloppy-org/slopask/internal/store"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "create-room":
		cmdCreateRoom(os.Args[2:])
	case "list-rooms":
		cmdListRooms(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: slopask <command> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  serve        start the web server")
	fmt.Fprintln(os.Stderr, "  create-room  create a new Q&A room")
	fmt.Fprintln(os.Stderr, "  list-rooms   list all rooms")
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	bind := fs.String("bind", "127.0.0.1", "bind address")
	port := fs.Int("port", 8430, "listen port")
	dataDir := fs.String("data-dir", "./data", "database directory")
	uploadsDir := fs.String("uploads-dir", "./uploads", "uploads directory")
	fs.Parse(args)

	st, err := store.Open(*dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := server.New(st, *uploadsDir)
	addr := fmt.Sprintf("%s:%d", *bind, *port)
	log.Printf("listening on %s", addr)
	if err := srv.Start(addr); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func cmdCreateRoom(args []string) {
	fs := flag.NewFlagSet("create-room", flag.ExitOnError)
	title := fs.String("title", "", "room title (required)")
	dataDir := fs.String("data-dir", "./data", "database directory")
	fs.Parse(args)

	if *title == "" {
		fmt.Fprintln(os.Stderr, "error: --title is required")
		fs.Usage()
		os.Exit(1)
	}

	st, err := store.Open(*dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	room, err := st.CreateRoom(*title)
	if err != nil {
		log.Fatalf("create room: %v", err)
	}

	fmt.Printf("slug:        %s\n", room.Slug)
	fmt.Printf("admin_token: %s\n", room.AdminToken)
	fmt.Printf("student url: /r/%s\n", room.Slug)
	fmt.Printf("admin url:   /admin/%s\n", room.AdminToken)
}

func cmdListRooms(args []string) {
	fs := flag.NewFlagSet("list-rooms", flag.ExitOnError)
	dataDir := fs.String("data-dir", "./data", "database directory")
	fs.Parse(args)

	st, err := store.Open(*dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	rooms, err := st.ListRooms()
	if err != nil {
		log.Fatalf("list rooms: %v", err)
	}

	if len(rooms) == 0 {
		fmt.Println("no rooms")
		return
	}
	for _, r := range rooms {
		fmt.Printf("%-14s %-26s %s\n", r.Slug, r.AdminToken, r.Title)
	}
}
