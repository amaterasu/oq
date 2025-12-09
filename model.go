package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

type viewMode int

const (
	viewEndpoints viewMode = iota
	viewComponents
	viewWebhooks
)

const keySequenceThreshold = 500 * time.Millisecond

const scrollHalfScreenLines = 21

// Layout constants (shared with view.go)
const (
	// Height

	headerApproxLines = 2 // Single line header + one empty line
	footerApproxLines = 4
	layoutBuffer      = 2 // Extra buffer to ensure header visibility

	// Width

	leftPaddingChars = 2 // "▶" + space
)

// calculateContentHeight returns the available height for content given the total viewport height
func calculateContentHeight(totalHeight int) int {
	return max(1, totalHeight-headerApproxLines-footerApproxLines-layoutBuffer)
}

func calculateContentWidth(totalWidth int) int {
	return max(1, totalWidth-leftPaddingChars)
}

type webhook struct {
	name   string
	method string
	op     *v3.Operation
	folded bool
}

type endpoint struct {
	path   string
	method string
	op     *v3.Operation
	folded bool
}

type component struct {
	name        string
	compType    string
	description string
	details     string
	folded      bool
}

type Model struct {
	doc                *v3.Document
	endpoints          []endpoint
	components         []component
	webhooks           []webhook
	cursor             int
	mode               viewMode
	width              int
	height             int
	showHelp           bool
	lastKey            string
	lastKeyAt          time.Time
	scrollOffset       int
	searchMode         bool
	searchInput        textinput.Model
	filteredEndpoints  []endpoint
	filteredComponents []component
	filteredWebhooks   []webhook
	showCurl           bool
	curlCommand        string
}

func (m *Model) getItemHeight(index int) int {
	switch m.mode {
	case viewEndpoints:
		eps := m.getActiveEndpoints()
		if index >= len(eps) {
			return 1
		}
		ep := eps[index]
		if ep.folded {
			return 1 // Just the main line when folded
		}
		// When unfolded, count main line + detail lines
		details := formatEndpointDetails(ep)
		return 1 + strings.Count(details, "\n") + 1 // +1 for main line, +1 for the detail section
	case viewComponents:
		comps := m.getActiveComponents()
		if index >= len(comps) {
			return 1
		}
		comp := comps[index]
		if comp.folded {
			return 1 // Just the main line when folded
		}
		// When unfolded, count main line + detail lines
		return 1 + strings.Count(comp.details, "\n") + 1 // +1 for main line, +1 for the detail section
	case viewWebhooks:
		hooks := m.getActiveWebhooks()
		if index >= len(hooks) {
			return 1
		}
		hook := hooks[index]
		if hook.folded {
			return 1 // Just the main line when folded
		}
		// When unfolded, count main line + detail lines
		details := formatWebhookDetails(hook)
		return 1 + strings.Count(details, "\n") + 1 // +1 for main line, +1 for the detail section
	}
	return 1
}

func (m *Model) getActiveEndpoints() []endpoint {
	if m.searchInput.Value() != "" {
		return m.filteredEndpoints
	}
	return m.endpoints
}

func (m *Model) getActiveComponents() []component {
	if m.searchInput.Value() != "" {
		return m.filteredComponents
	}
	return m.components
}

func (m *Model) getActiveWebhooks() []webhook {
	if m.searchInput.Value() != "" {
		return m.filteredWebhooks
	}
	return m.webhooks
}

func (m *Model) getMaxItems() int {
	switch m.mode {
	case viewEndpoints:
		return len(m.getActiveEndpoints()) - 1
	case viewComponents:
		return len(m.getActiveComponents()) - 1
	case viewWebhooks:
		return len(m.getActiveWebhooks()) - 1
	default:
		return -1
	}
}

func (m *Model) ensureCursorVisible() {
	// Calculate available content height using shared function
	contentHeight := calculateContentHeight(m.height)

	// Special case: if cursor is at 0, ensure we scroll to the very top
	if m.cursor == 0 {
		m.scrollOffset = 0
		return
	}

	// Calculate the actual rendered height of items to properly handle viewport
	var items []interface{}
	switch m.mode {
	case viewEndpoints:
		eps := m.getActiveEndpoints()
		for i := range eps {
			items = append(items, eps[i])
		}
	case viewComponents:
		comps := m.getActiveComponents()
		for i := range comps {
			items = append(items, comps[i])
		}
	case viewWebhooks:
		hooks := m.getActiveWebhooks()
		for i := range hooks {
			items = append(items, hooks[i])
		}
	}

	if len(items) == 0 {
		return
	}

	// Calculate lines used by items from scrollOffset to cursor
	linesUsed := 0

	// If cursor is above current scroll position, scroll up to show it
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
		return
	}

	// Calculate how many lines are used from scrollOffset to cursor (inclusive)
	for i := m.scrollOffset; i <= m.cursor && i < len(items); i++ {
		linesUsed += m.getItemHeight(i)
	}

	// Account for scroll indicators
	if m.scrollOffset > 0 {
		linesUsed++ // "More items above" indicator
	}

	// If the cursor item extends beyond available content height, scroll down
	if linesUsed > contentHeight {
		// Find the minimum scroll offset that keeps cursor visible
		for newScrollOffset := m.scrollOffset + 1; newScrollOffset <= m.cursor; newScrollOffset++ {
			testLinesUsed := 0

			// Account for "More items above" indicator
			if newScrollOffset > 0 {
				testLinesUsed++
			}

			// Calculate lines from new scroll offset to cursor
			for i := newScrollOffset; i <= m.cursor && i < len(items); i++ {
				testLinesUsed += m.getItemHeight(i)
			}

			if testLinesUsed <= contentHeight {
				m.scrollOffset = newScrollOffset
				break
			}
		}
	}

	// Ensure scroll offset doesn't go negative
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func generateExampleJSON(schema *base.Schema, doc *v3.Document, depth int) string {
	// Prevent infinite recursion
	if depth > 3 {
		return "null"
	}

	if schema == nil {
		return "{}"
	}

	// Handle schema with example
	if schema.Example != nil {
		return fmt.Sprintf("%v", schema.Example)
	}

	// Handle different schema types
	if len(schema.Type) > 0 {
		switch schema.Type[0] {
		case "object":
			var props []string
			if schema.Properties != nil {
				for pair := schema.Properties.First(); pair != nil; pair = pair.Next() {
					propName := pair.Key()
					propSchema := pair.Value()
					
					// Generate value for this property
					var value string
					if propSchema.Schema() != nil {
						value = generateExampleJSON(propSchema.Schema(), doc, depth+1)
					} else {
						value = "\"example\""
					}
					props = append(props, fmt.Sprintf("\"%s\": %s", propName, value))
				}
			}
			if len(props) > 0 {
				return "{ " + strings.Join(props, ", ") + " }"
			}
			return "{}"

		case "array":
			if schema.Items != nil && schema.Items.IsA() {
				itemSchema := schema.Items.A.Schema()
				if itemSchema != nil {
					return "[ " + generateExampleJSON(itemSchema, doc, depth+1) + " ]"
				}
			}
			return "[]"

		case "string":
			if len(schema.Enum) > 0 {
				return fmt.Sprintf("\"%v\"", schema.Enum[0])
			}
			if schema.Format == "date" {
				return "\"2024-01-01\""
			}
			if schema.Format == "date-time" {
				return "\"2024-01-01T00:00:00Z\""
			}
			if schema.Format == "email" {
				return "\"user@example.com\""
			}
			return "\"string\""

		case "number", "integer":
			return "0"

		case "boolean":
			return "false"

		case "null":
			return "null"
		}
	}

	// Handle $ref
	if len(schema.AllOf) > 0 {
		// For allOf, try to merge properties from all schemas
		var allProps []string
		for _, schemaProxy := range schema.AllOf {
			if schemaProxy.Schema() != nil {
				example := generateExampleJSON(schemaProxy.Schema(), doc, depth+1)
				// Extract properties from the example (simple approach)
				if example != "{}" && example != "null" {
					allProps = append(allProps, example)
				}
			}
		}
		if len(allProps) > 0 {
			return allProps[0] // Simplified - just use first one
		}
	}

	return "{}"
}

func generateCurl(ep endpoint, doc *v3.Document) string {
	var curl strings.Builder

	// Start with curl command
	curl.WriteString("curl -X " + ep.method)

	// Add URL - use first server if available, otherwise placeholder
	baseURL := "https://api.example.com"
	if len(doc.Servers) > 0 {
		baseURL = doc.Servers[0].URL
	}
	curl.WriteString(" '" + baseURL + ep.path + "'")

	// Add common headers
	headers := make(map[string]string)

	// Check if endpoint has request body (POST, PUT, PATCH typically)
	if ep.op.RequestBody != nil {
		headers["Content-Type"] = "application/json"
	}

	// Add security headers if defined
	if len(ep.op.Security) > 0 {
		// Check for common auth types
		for _, secReq := range ep.op.Security {
			for pair := secReq.Requirements.First(); pair != nil; pair = pair.Next() {
				secName := pair.Key()
				if doc.Components != nil && doc.Components.SecuritySchemes != nil {
					if scheme := doc.Components.SecuritySchemes.GetOrZero(secName); scheme != nil {
						switch scheme.Type {
						case "http":
							if scheme.Scheme == "bearer" {
								headers["Authorization"] = "Bearer YOUR_TOKEN"
							} else if scheme.Scheme == "basic" {
								headers["Authorization"] = "Basic YOUR_CREDENTIALS"
							}
						case "apiKey":
							if scheme.In == "header" {
								headers[scheme.Name] = "YOUR_API_KEY"
							}
						}
					}
				}
			}
		}
	}

	// Add headers to curl
	for key, value := range headers {
		curl.WriteString(" \\\n  -H '" + key + ": " + value + "'")
	}

	// Add request body example if present
	if ep.op.RequestBody != nil && ep.op.RequestBody.Content != nil {
		if jsonContent := ep.op.RequestBody.Content.GetOrZero("application/json"); jsonContent != nil {
			var bodyJSON string
			if jsonContent.Schema != nil && jsonContent.Schema.Schema() != nil {
				bodyJSON = generateExampleJSON(jsonContent.Schema.Schema(), doc, 0)
			} else {
				bodyJSON = "{}"
			}
			curl.WriteString(" \\\n  -d '" + bodyJSON + "'")
		}
	}

	return curl.String()
}

func NewModel(doc *v3.Document) Model {
	endpoints := extractEndpoints(doc)
	components := extractComponents(doc)
	webhooks := extractWebhooks(doc)

	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.CharLimit = 100
	ti.Width = 50

	return Model{
		doc:          doc,
		endpoints:    endpoints,
		components:   components,
		webhooks:     webhooks,
		cursor:       0,
		mode:         viewEndpoints,
		width:        80,
		height:       24,
		showHelp:     false,
		scrollOffset: 0,
		searchMode:   false,
		searchInput:  ti,
		showCurl:     false,
	}
}

func (m *Model) hasWebhooks() bool {
	return len(m.webhooks) > 0
}

func (m *Model) filterItems() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredEndpoints = nil
		m.filteredComponents = nil
		m.filteredWebhooks = nil
		return
	}

	// Filter endpoints
	m.filteredEndpoints = nil
	for _, ep := range m.endpoints {
		if strings.Contains(strings.ToLower(ep.path), query) ||
			strings.Contains(strings.ToLower(ep.method), query) ||
			(ep.op.Summary != "" && strings.Contains(strings.ToLower(ep.op.Summary), query)) ||
			(ep.op.Description != "" && strings.Contains(strings.ToLower(ep.op.Description), query)) {
			m.filteredEndpoints = append(m.filteredEndpoints, ep)
		}
	}

	// Filter components
	m.filteredComponents = nil
	for _, comp := range m.components {
		if strings.Contains(strings.ToLower(comp.name), query) ||
			strings.Contains(strings.ToLower(comp.compType), query) ||
			strings.Contains(strings.ToLower(comp.description), query) {
			m.filteredComponents = append(m.filteredComponents, comp)
		}
	}

	// Filter webhooks
	m.filteredWebhooks = nil
	for _, hook := range m.webhooks {
		if strings.Contains(strings.ToLower(hook.name), query) ||
			strings.Contains(strings.ToLower(hook.method), query) ||
			(hook.op.Summary != "" && strings.Contains(strings.ToLower(hook.op.Summary), query)) ||
			(hook.op.Description != "" && strings.Contains(strings.ToLower(hook.op.Description), query)) {
			m.filteredWebhooks = append(m.filteredWebhooks, hook)
		}
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		// Handle search mode input
		if m.searchMode {
			switch msg.String() {
			case "esc":
				// Esc clears search and exits search mode
				m.searchMode = false
				m.searchInput.SetValue("")
				m.filterItems()
				m.cursor = 0
				m.scrollOffset = 0
				return m, nil
			case "ctrl+c":
				// Ctrl+C quits the application
				return m, tea.Quit
			case "enter":
				// Enter keeps the filter and exits search mode
				m.searchMode = false
				m.searchInput.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				m.filterItems()
				m.cursor = 0
				m.scrollOffset = 0
				return m, cmd
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			if m.showHelp {
				m.showHelp = false
			} else {
				return m, tea.Quit
			}

		case "?":
			m.showHelp = !m.showHelp

		case "/":
			if !m.showHelp {
				m.searchMode = true
				m.searchInput.Focus()
				m.searchInput.SetValue("")
				m.filterItems()
				return m, nil
			}

		case "esc":
			if m.showHelp {
				m.showHelp = false
			} else if m.searchMode {
				m.searchMode = false
				m.searchInput.SetValue("")
				m.filterItems()
				m.cursor = 0
				m.scrollOffset = 0
			} else if m.showCurl {
				m.showCurl = false
			}

		case "r":
			if !m.showHelp && !m.searchMode {
				if m.mode == viewEndpoints {
					eps := m.getActiveEndpoints()
					if m.cursor < len(eps) {
						m.curlCommand = generateCurl(eps[m.cursor], m.doc)
						m.showCurl = true
					}
				} else if m.mode == viewWebhooks {
					hooks := m.getActiveWebhooks()
					if m.cursor < len(hooks) {
						// Create a temporary endpoint for webhook
						tempEp := endpoint{
							path:   hooks[m.cursor].name,
							method: hooks[m.cursor].method,
							op:     hooks[m.cursor].op,
						}
						m.curlCommand = generateCurl(tempEp, m.doc)
						m.showCurl = true
					}
				}
			}

		case "tab", "L":
			if !m.showHelp {
				// Cycle forward through available views
				switch m.mode {
				case viewEndpoints:
					if m.hasWebhooks() {
						m.mode = viewWebhooks
					} else {
						m.mode = viewComponents
					}
				case viewWebhooks:
					m.mode = viewComponents
				case viewComponents:
					m.mode = viewEndpoints
				}
				m.cursor = 0
				m.scrollOffset = 0
			}

		case "shift+tab", "H":
			if !m.showHelp {
				// Cycle backwards through available views
				switch m.mode {
				case viewEndpoints:
					m.mode = viewComponents
				case viewWebhooks:
					m.mode = viewEndpoints
				case viewComponents:
					if m.hasWebhooks() {
						m.mode = viewWebhooks
					} else {
						m.mode = viewEndpoints
					}
				}
				m.cursor = 0
				m.scrollOffset = 0
			}

		case "up", "k":
			if !m.showHelp && m.cursor > 0 {
				m.cursor--
				m.ensureCursorVisible()
			}

		case "down", "j":
			if !m.showHelp {
				if m.cursor < m.getMaxItems() {
					m.cursor++
					m.ensureCursorVisible()
				}
			}

		case "ctrl+d":
			if !m.showHelp {
				maxItems := m.getMaxItems()
				newCursorPos := m.cursor + scrollHalfScreenLines

				if newCursorPos > maxItems {
					m.cursor = maxItems
				} else {
					m.cursor += scrollHalfScreenLines
				}

				m.ensureCursorVisible()
			}

		case "ctrl+u":
			if !m.showHelp {
				halfLines := max(1, calculateContentHeight(m.height)/2)
				if m.cursor < halfLines {
					m.cursor = 0
				} else {
					m.cursor -= halfLines
				}

				m.ensureCursorVisible()
			}

		case "G":
			if !m.showHelp {
				maxItems := m.getMaxItems()
				if maxItems >= 0 {
					m.cursor = maxItems
					m.ensureCursorVisible()
				}
			}

		case "g":
			now := time.Now()
			if m.lastKey == "g" && now.Sub(m.lastKeyAt) < keySequenceThreshold {
				if !m.showHelp {
					m.cursor = 0
					m.ensureCursorVisible()
				}

				// reset, so "ggg" wouldn't be triggered
				m.lastKey = ""
				m.lastKeyAt = time.Time{}

			} else {
				m.lastKey = "g"
				m.lastKeyAt = now
			}

		case "enter", " ":
			if !m.showHelp && !m.searchMode {
				if m.mode == viewEndpoints {
					eps := m.getActiveEndpoints()
					if m.cursor < len(eps) {
						// Toggle the folded state in the source list
						for i := range m.endpoints {
							if m.endpoints[i].path == eps[m.cursor].path && m.endpoints[i].method == eps[m.cursor].method {
								m.endpoints[i].folded = !m.endpoints[i].folded
								m.filterItems() // Refresh filtered list
								break
							}
						}
					}
				} else if m.mode == viewComponents {
					comps := m.getActiveComponents()
					if m.cursor < len(comps) {
						for i := range m.components {
							if m.components[i].name == comps[m.cursor].name {
								m.components[i].folded = !m.components[i].folded
								m.filterItems()
								break
							}
						}
					}
				} else if m.mode == viewWebhooks {
					hooks := m.getActiveWebhooks()
					if m.cursor < len(hooks) {
						for i := range m.webhooks {
							if m.webhooks[i].name == hooks[m.cursor].name && m.webhooks[i].method == hooks[m.cursor].method {
								m.webhooks[i].folded = !m.webhooks[i].folded
								m.filterItems()
								break
							}
						}
					}
				}
			}
		}
	}

	return m, nil
}

// truncateContent ensures content doesn't exceed the available lines
func (m Model) truncateContent(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}

	// Truncate to fit and add an indicator
	truncatedLines := lines[:maxLines-1]
	truncatedLines = append(truncatedLines, "⬇ Content truncated to fit viewport...")

	return strings.Join(truncatedLines, "\n")
}

func (m Model) View() string {
	var s strings.Builder

	header := m.renderHeader()
	footer := m.renderFooter()

	headerLines := strings.Count(header, "\n")
	footerLines := strings.Count(footer, "\n")

	// Calculate how many lines are available for content
	availableContentLines := m.height - headerLines - footerLines - 1
	if availableContentLines < 1 {
		availableContentLines = 1
	}

	// Render content
	var content string
	switch m.mode {
	case viewEndpoints:
		content = m.renderEndpoints()
	case viewComponents:
		content = m.renderComponents()
	case viewWebhooks:
		content = m.renderWebhooks()
	}

	// Truncate content if it's too long
	content = m.truncateContent(content, availableContentLines)

	s.WriteString(header)
	s.WriteString(content)

	contentLines := strings.Count(content, "\n")
	usedLines := headerLines + contentLines + footerLines
	remainingLines := m.height - usedLines - 1

	if remainingLines > 0 {
		s.WriteString(strings.Repeat("\n", remainingLines))
	}

	s.WriteString(footer)

	baseView := s.String()

	if m.showHelp {
		return m.renderHelpModal()
	}

	if m.showCurl {
		return m.renderCurlModal()
	}

	return baseView
}
