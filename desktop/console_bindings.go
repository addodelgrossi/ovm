package main

import core "github.com/addodelgrossi/ovm/internal/app"

// soapConsoleCapacity bounds the in-memory SOAP console.
const soapConsoleCapacity = 500

type SetConsoleEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

type ConsoleEntriesRequest struct {
	AfterID uint64 `json:"after_id"`
}

// ConsoleState is the capture state and the entries newer than the request.
type ConsoleState struct {
	Enabled bool                    `json:"enabled"`
	Entries []core.SOAPConsoleEntry `json:"entries"`
}

type ConsoleResponse struct {
	Result *ConsoleState `json:"result,omitempty"`
	Error  *BindingError `json:"error,omitempty"`
}

func (a *App) soapConsole() (*core.SOAPConsole, *BindingError) {
	a.mu.Lock()
	console, closed := a.console, a.closed
	a.mu.Unlock()
	if closed {
		return nil, bindingUnavailable("app_closed", "the desktop application is closed")
	}
	if console == nil {
		return nil, bindingUnavailable("console_unavailable", "the SOAP console is not available")
	}
	return console, nil
}

// SetConsoleEnabled turns SOAP capture on or off. Turning it off discards the
// captured exchanges.
func (a *App) SetConsoleEnabled(request SetConsoleEnabledRequest) ConsoleResponse {
	console, bindingErr := a.soapConsole()
	if bindingErr != nil {
		return ConsoleResponse{Error: bindingErr}
	}
	console.SetEnabled(request.Enabled)
	return ConsoleResponse{Result: &ConsoleState{Enabled: console.Enabled(), Entries: console.Entries(0)}}
}

// ConsoleEntries returns the redacted exchanges captured after AfterID.
func (a *App) ConsoleEntries(request ConsoleEntriesRequest) ConsoleResponse {
	console, bindingErr := a.soapConsole()
	if bindingErr != nil {
		return ConsoleResponse{Error: bindingErr}
	}
	return ConsoleResponse{Result: &ConsoleState{Enabled: console.Enabled(), Entries: console.Entries(request.AfterID)}}
}

// ClearConsole discards the captured exchanges and keeps capture state.
func (a *App) ClearConsole() ConsoleResponse {
	console, bindingErr := a.soapConsole()
	if bindingErr != nil {
		return ConsoleResponse{Error: bindingErr}
	}
	console.Clear()
	return ConsoleResponse{Result: &ConsoleState{Enabled: console.Enabled(), Entries: []core.SOAPConsoleEntry{}}}
}
