package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

)

//go:embed static
var staticFiles embed.FS

const usageText = `Usage: scrumban [--port PORT] [--help] <data-directory>

Arguments:
  <data-directory>   Path to the directory containing board column folders.
                     Column folders must be named in the format ##.Name
                     (e.g. 01.Backlog, 02.In Progress, 03.Done).
                     Each folder contains .md files representing stories.

Flags:
  --port PORT        TCP port to listen on. If omitted, a free port is chosen
                     automatically and printed to stdout.
  --help             Show this help message and exit.

Story files:
  Each .md file must begin with YAML front matter containing:
    assignee, status, priority
  Followed by a Markdown body where the first # heading is the story summary,
  the text after it is the description, and the second # heading begins the
  comment section.

Example:
  scrumban ./my-project-board
  scrumban --port 9090 /home/user/board
`

func main() {
	var port int
	var help bool

	flag.IntVar(&port, "port", 0, "port to listen on (0 = auto)")
	flag.BoolVar(&help, "help", false, "show help")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usageText)
	}
	flag.Parse()

	if help {
		fmt.Print(usageText)
		os.Exit(0)
	}

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: data directory argument is required.\n")
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(1)
	}

	dataDir := flag.Arg(0)

	// Verify the directory exists.
	info, err := os.Stat(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot access data directory %q: %v\n", dataDir, err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %q is not a directory\n", dataDir)
		os.Exit(1)
	}

	// Bind the port before starting the HTTP server so we know the address.
	listener, err := newListener(port)
	if err != nil {
		log.Fatalf("failed to bind port: %v", err)
	}

	addr := listener.Addr().(*net.TCPAddr)
	serverAddr := addr.String()
	url := fmt.Sprintf("http://localhost:%d", addr.Port)

	// Set up routes.
	mux := http.NewServeMux()

	srv := &Server{DataDir: dataDir}
	srv.RegisterRoutes(mux)

	// Serve static files (the frontend) embedded in the binary.
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("failed to create static sub-filesystem: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	fmt.Printf("ScrumBan running at %s\n", url)
	fmt.Printf("Data directory: %s\n", dataDir)

	// Open the browser once the server is confirmed to be accepting connections.
	go func() {
		waitForServer(serverAddr)
		openBrowser(url)
	}()

	if err := http.Serve(listener, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// waitForServer polls until the TCP port is accepting connections or 5 seconds
// elapse, whichever comes first.
func waitForServer(addr string) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func newListener(port int) (net.Listener, error) {
	addr := fmt.Sprintf(":%d", port)
	return net.Listen("tcp", addr)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open browser: %v", err)
	}
}
