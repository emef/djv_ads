package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/emef/djv_ads"
	"github.com/golang/glog"
)

type State struct {
	Undercut       float64
	RunEvery       int
	DebugEnabled   string
	Enabled        string
	StatusBtnClass string
	StatusText     string
	LastUpdated    string
}

const UNDERCUT_DEFAULT = 0.001
const RUNEVERY_DEFAULT = 15
const ENABLED = "checked"
const DISABLED = ""
const STATUS_ENABLED_CLASS = "btn-success"
const STATUS_DISABLED_CLASS = "btn-danger"
const STATUS_ENABLED_TEXT = "running"
const STATUS_DISABLED_TEXT = "paused"
const LAST_UPDATED_DEFAULT = "unknown"

const INDEX_TEMPLATE = "index.template.html"

var (
	readOnly = flag.Bool("readonly", false,
		"If set to true, will only calculate updates but not execute them ")
	statePath = flag.String("state_path", "",
		"Path to config file (will create with defaults if missing)")
	templatesDir = flag.String("templates_dir", "",
		"Directory containing djv_ads templates")
	updatesLogPath = flag.String("updates_path", "",
		"Path to write update logs to (no logging done if empty)")
	maxUpdateHistory = flag.Int("max_update_history", 200,
		"Maximum number of recent updates to show")
)

func main() {
	flag.Parse()

	go controllerCron()

	http.HandleFunc("/", handleUI)
	http.HandleFunc("/update", handleUpdate)
	http.ListenAndServe(":8081", nil)
}

func controllerCron() {
	for {
		glog.Infof("Controller cron started")
		state := getStateOrDefault(*statePath)

		if state.Enabled == ENABLED {
			glog.Infof("Starting controller cron")

			opts := []djv_ads.Option{
				djv_ads.ReadOnly(*readOnly),
				djv_ads.UndercutBy(state.Undercut),
			}

			if true || state.DebugEnabled == ENABLED {
				opts = append(opts, djv_ads.WithCampaignWhitelist(
					"1002170291", "1002170211", "1002170181", "1002170171"))
			}

			controller, err := djv_ads.NewAdsController(opts...)
			if err != nil {
				glog.Errorf("Error creating ads controller: %v", err)
				continue
			}

			updates := controller.RunOnce()

			if err = writeUpdatesToLog(*updatesLogPath, updates); err != nil {
				glog.Errorf("Error writing to updates log file: %v", err)
			}

			state := getStateOrDefault(*statePath)
			state.LastUpdated = time.Now().In(djv_ads.Pacific).Format(djv_ads.TimeFormat)
			writeState(*statePath, state)
		} else {
			glog.Infof("Controller disabled, skipping run")
		}

		glog.Infof("Finished Controller cron")
		time.Sleep(time.Duration(state.RunEvery) * time.Minute)
	}
}

func handleUI(w http.ResponseWriter, r *http.Request) {
	indexTemplatePath := path.Join(*templatesDir, INDEX_TEMPLATE)
	template, err := template.ParseFiles(indexTemplatePath)
	if err != nil {
		handleError(w, fmt.Sprintf("couldn't read template %s: %v",
			indexTemplatePath, err))
		return
	}

	state := getStateOrDefault(*statePath)
	updates, err := readUpdatesLog(*updatesLogPath)
	if err != nil {
		glog.Errorf("Error reading updates log file :%v", err)
	}

	// reverse updates so newest at top
	for i, j := 0, len(updates)-1; i < j; i, j = i+1, j-1 {
		updates[i], updates[j] = updates[j], updates[i]
	}

	if len(updates) > *maxUpdateHistory {
		updates = updates[:*maxUpdateHistory]
	}

	context := struct {
		State   *State
		Updates []*djv_ads.BidUpdate
	}{state, updates}

	template.Execute(w, context)
}

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	undercutStr := query.Get("undercut")
	runeveryStr := query.Get("runevery")
	debugEnabledStr := query.Get("debugenabled")
	enabledStr := query.Get("enabled")

	state := getStateOrDefault(*statePath)
	undercut, err := strconv.ParseFloat(undercutStr, 64)
	if err != nil {
		handleError(w, fmt.Sprintf("could not parse undercut float: %v", undercutStr))
		return
	}

	runevery, err := strconv.Atoi(runeveryStr)
	if err != nil {
		handleError(w, fmt.Sprintf("could not parse runevery integer: %v", runeveryStr))
		return
	}

	state.Undercut = undercut
	state.RunEvery = runevery

	if debugEnabledStr == "on" {
		state.DebugEnabled = ENABLED
	} else {
		state.DebugEnabled = DISABLED
	}

	if enabledStr == "on" {
		state.Enabled = ENABLED
		state.StatusBtnClass = STATUS_ENABLED_CLASS
		state.StatusText = STATUS_ENABLED_TEXT
	} else {
		state.Enabled = DISABLED
		state.StatusBtnClass = STATUS_DISABLED_CLASS
		state.StatusText = STATUS_DISABLED_TEXT
	}

	if err = writeState(*statePath, state); err != nil {
		handleError(w, fmt.Sprintf("could not update state: %v", err))
		return
	}

	// redirect
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func handleError(w http.ResponseWriter, errorText string) {
	glog.Errorf("Unexpected error: %v", errorText)
	fmt.Fprintf(w, errorText)
}

func getStateOrDefault(path string) *State {
	state, err := readState(*statePath)
	if err != nil {
		glog.Error("Error reading state: %v", err)
		state = defaultState()
	}

	return state
}

func readState(path string) (*State, error) {
	state := &State{}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err = decoder.Decode(state); err != nil {
		return nil, err
	}

	return state, nil
}

func writeState(path string, state *State) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	return encoder.Encode(state)
}

func readUpdatesLog(path string) ([]*djv_ads.BidUpdate, error) {
	if path == "" {
		return nil, nil
	}

	logsFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer logsFile.Close()

	decoder := json.NewDecoder(logsFile)

	updates := make([]*djv_ads.BidUpdate, 0)
	for {
		update := &djv_ads.BidUpdate{}
		err = decoder.Decode(update)
		if err == io.EOF {
			return updates, nil
		} else if err != nil {
			return nil, err
		}

		updates = append(updates, update)
	}
}

func writeUpdatesToLog(path string, updates []*djv_ads.BidUpdate) error {
	if path == "" {
		return nil
	}

	logsFile, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0755)
	if err != nil {
		return err
	}
	defer logsFile.Close()

	encoder := json.NewEncoder(logsFile)

	for _, update := range updates {
		if err = encoder.Encode(update); err != nil {
			return err
		}
	}

	return nil
}

func defaultState() *State {
	return &State{
		Undercut:       UNDERCUT_DEFAULT,
		RunEvery:       RUNEVERY_DEFAULT,
		DebugEnabled:   ENABLED,
		Enabled:        DISABLED,
		StatusBtnClass: STATUS_DISABLED_CLASS,
		StatusText:     STATUS_DISABLED_TEXT,
		LastUpdated:    LAST_UPDATED_DEFAULT,
	}
}
