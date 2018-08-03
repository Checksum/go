// Package sse implements a server-sent events broker and listener
// Based on https://gist.github.com/schmohlio/d7bdb255ba61d3f5e51a512a7c0d6a85
package sse

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// the amount of time to wait when pushing a message to
// a slow client or a client that closed after `range clients` started.
const patience time.Duration = time.Second * 1

type streamFunc func(*http.Request) string

// Message defines a message to be sent to the clients
type Message struct {
	Stream string
	Body   []byte
}

type client struct {
	stream  string
	channel chan []byte
}

// Notifier represents the SSE broker
type Notifier struct {
	// Events are pushed to this channel by the main events-gathering routine
	Notify chan Message

	// New client connections
	newClients chan *client

	// Closed client connections
	closingClients chan *client

	// Client connections registry
	clients map[string]map[*client]bool

	// Function to generate stream id given the http request
	getstream streamFunc
}

// New returns a new broker
func New(getstream streamFunc) *Notifier {
	// Instantiate a broker
	broker := &Notifier{
		Notify:         make(chan Message, 1),
		newClients:     make(chan *client),
		closingClients: make(chan *client),
		clients:        make(map[string]map[*client]bool),
		getstream:      getstream,
	}

	// Set it running - listening and broadcasting events
	go broker.listen()

	return broker
}

func (broker *Notifier) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Make sure that the writer supports flushing.
	flusher, ok := rw.(http.Flusher)

	if !ok {
		http.Error(rw, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	h := rw.Header()

	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("Access-Control-Allow-Origin", "*")

	// Each connection registers its own message channel with the Broker's connections registry
	sseClient := &client{
		stream:  broker.getstream(req),
		channel: make(chan []byte),
	}

	// Signal the broker that we have a new connection
	broker.newClients <- sseClient

	// Remove this client from the map of connected clients
	// when this handler exits.
	defer func() {
		broker.closingClients <- sseClient
	}()

	// Listen to connection close and un-register sseClient
	notify := rw.(http.CloseNotifier).CloseNotify()

	for {
		select {
		case <-notify:
			return
		case message, open := <-sseClient.channel:
			if !open {
				break
			}
			// Write to the ResponseWriter
			// Server Sent Events compatible
			fmt.Fprintf(rw, "data: %s\n\n", message)

			// Flush the data immediatly instead of buffering it for later.
			flusher.Flush()
		}
	}
}

func (broker *Notifier) listen() {
	for {
		select {
		case newClient := <-broker.newClients:
			stream := newClient.stream
			// A new client has connected.
			// Register their message channel
			if _, ok := broker.clients[stream]; !ok {
				broker.clients[stream] = make(map[*client]bool, 0)
			}
			broker.clients[stream][newClient] = true

		case closedClient := <-broker.closingClients:
			stream := closedClient.stream
			// A client has dettached and we want to
			// stop sending them messages.
			delete(broker.clients[stream], closedClient)
			if len(broker.clients[stream]) == 0 {
				delete(broker.clients, stream)
			}
			close(closedClient.channel)

		case event := <-broker.Notify:
			// Send the message to all connected channels of the user
			// Send event to all connected clients
			for userClient := range broker.clients[event.Stream] {
				select {
				case userClient.channel <- event.Body:
				case <-time.After(patience):
					log.Printf("Skipping client %s due to timeout\n", userClient.stream)
				}
			}
		}
	}
}

// Send dispatches the message to the given channel
func (broker *Notifier) Send(stream string, body []byte) {
	broker.Notify <- Message{
		Stream: stream,
		Body:   body,
	}
}
