package main

import (
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
)

func main() {
	var content []byte
	var err error

	if len(os.Args) > 1 {
		content, err = os.ReadFile(os.Args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
			os.Exit(1)
		}
	} else {
		content, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading from stdin: %v\n", err)
			os.Exit(1)
		}
	}

	config := &datamodel.DocumentConfiguration{
		AllowFileReferences:   false,
		AllowRemoteReferences: false,
		BypassDocumentCheck:   true, // Allow parsing specs with errors
	}

	document, err := libopenapi.NewDocumentWithConfiguration(content, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating document: %v\n", err)
		os.Exit(1)
	}

	v3Model, err := document.BuildV3Model()
	if err != nil {
		// Show warning but try to continue if we have any model
		fmt.Fprintf(os.Stderr, "Warning: Spec has validation errors: %v\n", err)
		fmt.Fprintf(os.Stderr, "Attempting to continue with partial data...\n\n")

		// If we can't build the model at all, exit
		if v3Model == nil {
			os.Exit(1)
		}
	}

	m := NewModel(&v3Model.Model)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
