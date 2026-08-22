package everything

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/agent"
	everythingapi "github.com/beyondmarks-ai/Wrapper/src/pkg/everything"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/localagent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type Model struct {
	headline      string
	searcher      everythingapi.Searcher
	startupError  error
	remoteClient  RemoteClient
	devices       []remote.Device
	deviceError   error
	deviceIndex   int
	sendPaths     []string
	selected      map[string]struct{}
	progress      []agent.Progress
	progressError error

	open        bool
	justOpened  bool
	loading     bool
	textInput   textinput.Model
	results     []everythingapi.Result
	queryError  error
	cursor      int
	renderIndex int
	searchMode  SearchMode

	width     int
	maxHeight int
	reqCnt    int
}

type SearchMode uint8

const (
	SearchAll SearchMode = iota
	SearchFiles
	SearchFolders
)

func (mode SearchMode) Label() string {
	switch mode {
	case SearchFiles:
		return "FILES"
	case SearchFolders:
		return "FOLDERS"
	default:
		return "ALL"
	}
}

func (mode SearchMode) APIValue() string {
	switch mode {
	case SearchFiles:
		return "file"
	case SearchFolders:
		return "folder"
	default:
		return "all"
	}
}

func (mode SearchMode) EverythingQuery(query string) string {
	switch mode {
	case SearchFiles:
		return "file: " + query
	case SearchFolders:
		return "folder: " + query
	default:
		return query
	}
}

func (mode SearchMode) FilterResults(results []everythingapi.Result) []everythingapi.Result {
	if mode == SearchAll {
		return results
	}

	filtered := make([]everythingapi.Result, 0, len(results))
	for _, result := range results {
		if mode == SearchFolders && result.IsDir || mode == SearchFiles && !result.IsDir {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

type QueryMsg struct {
	query          string
	searchQuery    string
	mode           SearchMode
	reqID          int
	remoteDeviceID string
}

func (msg QueryMsg) Apply(m *Model) tea.Cmd {
	if msg.mode != m.searchMode || msg.query != strings.TrimSpace(m.textInput.Value()) ||
		msg.searchQuery != m.searchMode.EverythingQuery(msg.query) {
		return nil
	}
	return func() tea.Msg {
		var results []everythingapi.Result
		var err error
		if msg.remoteDeviceID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			results, err = m.remoteSearch(ctx, localagent.SearchInput{
				DeviceID: msg.remoteDeviceID,
				Query:    msg.query,
				Mode:     msg.mode.APIValue(),
				Limit:    everythingapi.DefaultMaxResults,
			})
		} else {
			results, err = m.searcher.Search(msg.searchQuery, everythingapi.DefaultMaxResults)
		}
		results = msg.mode.FilterResults(results)
		return UpdateMsg{query: msg.query, searchQuery: msg.searchQuery, results: results, err: err, reqID: msg.reqID, remoteDeviceID: msg.remoteDeviceID}
	}
}

type UpdateMsg struct {
	query          string
	searchQuery    string
	results        []everythingapi.Result
	err            error
	reqID          int
	remoteDeviceID string
}

func NewUpdateMsg(query string, results []everythingapi.Result, err error, reqID int) UpdateMsg {
	return UpdateMsg{query: query, searchQuery: query, results: results, err: err, reqID: reqID}
}

func (msg UpdateMsg) GetReqID() int { return msg.reqID }
