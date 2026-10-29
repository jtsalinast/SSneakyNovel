package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"showmethestory/internal/config"
	"showmethestory/internal/devlog"
	"showmethestory/internal/httpapi"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
)

//go:embed frontend/dist
var staticFiles embed.FS

// version is injected by CI via -ldflags "-X main.version=...".
var version = "dev"

const defaultPort = ":48090"

func main() {
	// Determine program directory (progDir)
	// Priority: os.Args[1] if it's a valid existing directory, otherwise use cwd
	progDir := ""

	if len(os.Args) > 1 {
		absDir, err := filepath.Abs(os.Args[1])
		if err == nil {
			if info, err := os.Stat(absDir); err == nil && info.IsDir() {
				progDir = absDir
			}
		}
	}

	if progDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Printf(" [] nowithdrawwhenfront: %v\n", err)
			os.Exit(1)
		}
		progDir = cwd
	}

	// Create storys directory
	storysDir := filepath.Join(progDir, "storys")
	os.MkdirAll(storysDir, 0755)

	// Load API config (global, shared across projects; always in progDir)
	apiCfgPath := filepath.Join(progDir, "api.json")
	apiCfg, err := config.LoadAPIConfig(apiCfgPath)
	if err != nil {
		fmt.Printf(" [] APIplace: %v\n", err)
		os.Exit(1)
	}
	llm.EnsureContextBudget(apiCfg)
	if apiCfg.Brainstorm != nil {
		llm.EnsureContextBudget(apiCfg.Brainstorm)
	}

	if apiCfg.BaseURL == "" || apiCfg.Model == "" {
		fmt.Println(" [is] toemptywhiteAPIplace, alreadymovelife api.json")
		fmt.Println(" [is] pleasepass Web UI place API andlateragainmakeuse")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	} else {
		port = ":" + port
	}

	logger := sse.NewLogBroadcaster()
	defer logger.Close()

	devlog.Init(progDir, version)

	fmt.Printf(" [is] this: %s\n", version)
	fmt.Printf(" [is] order: %s\n", progDir)
	fmt.Printf(" [is] item: %s\n", storysDir)
	if devlog.Enabled() {
		fmt.Printf(" [is] openhairsunambition: %s\n", filepath.Join(progDir, "dev.log"))
	}

	staticFS, err := fs.Sub(staticFiles, "frontend/dist")
	if err != nil {
		log.Fatalf("enterquietpostureitem: %v", err)
	}

	httpapi.StartWebServer(apiCfg, apiCfgPath, logger, port, progDir, version, staticFS)
}
