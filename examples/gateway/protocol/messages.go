// Package protocol defines only this example's request and response.
package protocol

//go:generate go run ../../../cmd/orbitgen -in messages.json -out bindings_gen.go

// Echo is the demonstration request (message ID 1).
type Echo struct {
	Text string `json:"text"`
}

// EchoReply is the demonstration response (message ID 2).
type EchoReply struct {
	Text    string `json:"text"`
	Backend string `json:"backend"`
}
