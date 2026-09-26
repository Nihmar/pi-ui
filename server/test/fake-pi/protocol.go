package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Record types the harness recognises or emits.
const (
	recordResponse            = "response"
	recordParse               = "parse"
	recordMessageUpdate       = "message_update"
	recordExtensionUIResponse = "extension_ui_response"
)

// Commands with a built-in default answer (docs/spike-interfaces.md §9).
const (
	commandGetState    = "get_state"
	commandGetEntries  = "get_entries"
	commandGetCommands = "get_commands"
	commandPrompt      = "prompt"
)

// responseRecord is the answer envelope, field order included, as pi writes it.
type responseRecord struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// command is the part of an inbound record the harness reacts to.
type command struct {
	ID    string
	Type  string
	Since string
}

// serve answers commands until stdin reaches EOF; EOF is pi's orderly shutdown.
func serve(in *bufio.Reader, em *emitter, script *Script, cfg config) error {
	for {
		line, err := readRecord(in)
		if len(line) > 0 {
			if handleErr := handleLine(line, em, script, cfg); handleErr != nil {
				return handleErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read stdin: %w", err)
		}
	}
}

// readRecord reads one LF-terminated record. Splitting happens on 0x0A only, so
// U+2028/U+2029 inside a JSON string never start a record; a trailing CR is stripped and
// empty lines are skipped. The final record of a stream without a terminator is still
// delivered, together with io.EOF.
func readRecord(in *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := in.ReadSlice('\n')
		buf = append(buf, chunk...)
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil && !errors.Is(err, io.EOF):
			return nil, err
		}
		trimmed := trimTerminator(buf)
		if err != nil {
			if len(trimmed) == 0 {
				return nil, io.EOF
			}
			return append([]byte(nil), trimmed...), io.EOF
		}
		if len(trimmed) == 0 {
			return nil, nil
		}
		return append([]byte(nil), trimmed...), nil
	}
}

func trimTerminator(line []byte) []byte {
	line = trimSuffixByte(line, '\n')
	return trimSuffixByte(line, '\r')
}

func trimSuffixByte(line []byte, b byte) []byte {
	if len(line) > 0 && line[len(line)-1] == b {
		return line[:len(line)-1]
	}
	return line
}

func handleLine(line []byte, em *emitter, script *Script, cfg config) error {
	cmd, err := parseCommand(line)
	if err != nil {
		return em.writeParseError(err)
	}
	if cmd.Type == recordExtensionUIResponse {
		return nil // answers a dialog: consumed silently, never answered
	}
	return answer(cmd, em, script, cfg)
}

func parseCommand(line []byte) (command, error) {
	var record struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Since string `json:"since"`
	}
	if err := json.Unmarshal(line, &record); err != nil {
		return command{}, fmt.Errorf("record is not a JSON object: %w", err)
	}
	if record.Type == "" {
		return command{}, fmt.Errorf("record carries no type")
	}
	return command{ID: record.ID, Type: record.Type, Since: record.Since}, nil
}

// answer emits the response for one command and, for a prompt, the synthetic run.
// A script entry for the command type wins over the built-in default; its events
// follow the response in order, each after its own delay.
func answer(cmd command, em *emitter, script *Script, cfg config) error {
	sleepMs(stallFor(script, cfg))

	cs, scripted := script.Commands[cmd.Type]
	if !scripted && script.Default != nil {
		// The script-wide default entry answers commands the script does not know.
		cs, scripted = *script.Default, true
	}
	if scripted {
		sleepMs(cs.DelayMs)
	}
	response, err := responseFor(cmd, cs, scripted, script)
	if err != nil {
		return err
	}
	if err := em.writeRecord(response); err != nil {
		return err
	}
	if scripted {
		for _, step := range cs.Events {
			sleepMs(step.DelayMs)
			if err := em.writeRecord(step.Record); err != nil {
				return err
			}
		}
	}
	if cmd.Type == commandPrompt {
		if err := em.emitSyntheticRun(cfg); err != nil {
			return err
		}
	}
	if scripted && cs.ExitCode != nil {
		// Deterministic fault: the answer (and its events) are on stdout before the
		// process ends, because exit waits for the record in flight.
		em.exit(*cs.ExitCode)
	}
	return nil
}

// stallFor is the longer of --stall-ms and the script's faults.stallMs: the flag is a
// floor, so setting both never doubles the delay.
func stallFor(script *Script, cfg config) int {
	if script.Faults.StallMs > cfg.stallMs {
		return script.Faults.StallMs
	}
	return cfg.stallMs
}

// responseFor picks the answer: a scripted failure, a scripted response (auto-filled),
// or the built-in default for that command type.
func responseFor(cmd command, cs CommandScript, scripted bool, script *Script) (json.RawMessage, error) {
	switch {
	case scripted && cs.Error != "":
		return marshalResponse(failureResponse(cmd, cs.Error))
	case scripted && len(cs.Response) > 0:
		return autoFilledResponse(cmd, cs.Response)
	default:
		return builtinResponse(cmd, script)
	}
}

// builtinResponse is the canned answer for commands the script says nothing about.
func builtinResponse(cmd command, script *Script) (json.RawMessage, error) {
	record := responseRecord{ID: cmd.ID, Type: recordResponse, Command: cmd.Type, Success: true}
	switch cmd.Type {
	case commandGetState:
		data, err := json.Marshal(script.cannedState())
		if err != nil {
			return nil, err
		}
		record.Data = data
	case commandGetEntries:
		entries, leaf, err := script.entriesPage(cmd.Since)
		if err != nil {
			return marshalResponse(failureResponse(cmd, err.Error()))
		}
		if entries == nil {
			entries = []json.RawMessage{}
		}
		data, err := json.Marshal(entriesData{Entries: entries, LeafID: leaf})
		if err != nil {
			return nil, err
		}
		record.Data = data
	case commandGetCommands:
		record.Data = json.RawMessage(`{"commands":[]}`)
	}
	return marshalResponse(record)
}

func failureResponse(cmd command, message string) responseRecord {
	return responseRecord{ID: cmd.ID, Type: recordResponse, Command: cmd.Type, Success: false, Error: message}
}

// autoFilledResponse fills the request id, the command name and the record type into a
// scripted response that omitted them; every other field stays byte-for-byte.
func autoFilledResponse(cmd command, response json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response, &fields); err != nil {
		return nil, fmt.Errorf("scripted response: %w", err)
	}
	fill := func(key, value string) error {
		if _, ok := fields[key]; ok {
			return nil
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		fields[key] = encoded
		return nil
	}
	if cmd.ID != "" {
		if err := fill("id", cmd.ID); err != nil {
			return nil, err
		}
	}
	if err := fill("command", cmd.Type); err != nil {
		return nil, err
	}
	if err := fill("type", recordResponse); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func marshalResponse(record responseRecord) (json.RawMessage, error) {
	return json.Marshal(record)
}

// startFaultTimers arms the wall-clock faults from process start; the CLI flag wins
// over the script value, and both wait for the record in flight before exiting.
func startFaultTimers(cfg config, script *Script, em *emitter) {
	if ms := firstSet(cfg.crashAfter, script.Faults.CrashAfterMs); ms >= 0 {
		time.AfterFunc(time.Duration(ms)*time.Millisecond, func() { em.exit(exitCrash) })
	}
	if ms := firstSet(cfg.exitAfter, script.Faults.ExitAfterMs); ms >= 0 {
		time.AfterFunc(time.Duration(ms)*time.Millisecond, func() { em.exit(exitOK) })
	}
}

func firstSet(flagMs int, scriptMs *int) int {
	if flagMs >= 0 {
		return flagMs
	}
	if scriptMs != nil {
		return *scriptMs
	}
	return unsetMs
}
