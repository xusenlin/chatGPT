package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/sashabaranov/go-openai"
	"io"
	"io/ioutil"
	"log"
	"net/http"
)

var Ai = openai.NewClient("your token")

type EventStream struct {
	Event string
	Data  string
}
type DialogMsg = map[string]chan EventStream

var ResponseEventStream = make(DialogMsg)

//go:embed index.html
var htmlTemplate string

func main() {

	http.HandleFunc("/", IndexHandler)
	http.HandleFunc("/send", SendMsgHandler)
	http.HandleFunc("/receive", ReceiveHandler)
	fmt.Println("start chatGPT service on 8088")
	log.Fatal(http.ListenAndServe(":8088", nil))
}

func SendMsgHandler(w http.ResponseWriter, r *http.Request) {

	data, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	var msg struct {
		Uuid string                         `json:"uuid"`
		Chat []openai.ChatCompletionMessage `json:"chat"`
	}

	err = json.Unmarshal(data, &msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	_, ok := ResponseEventStream[msg.Uuid]

	if len(msg.Uuid) != 36 || !ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("err:uuid出错"))
		return
	}
	if len(msg.Chat) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("err:问题为空"))
		return
	}

	ioutil.WriteFile(msg.Uuid+".json", data, 0666)

	ctx := context.Background()

	stream, err := Ai.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model:     openai.GPT3Dot5Turbo,
		MaxTokens: 4080,
		Stream:    true,
		Messages:  msg.Chat,
	})

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	w.Write([]byte("ok"))

	go func() {
		defer stream.Close()
		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				ResponseEventStream[msg.Uuid] <- EventStream{
					Event: "eof",
					Data:  err.Error(),
				}
				return
			}
			if err != nil {
				ResponseEventStream[msg.Uuid] <- EventStream{
					Event: "error",
					Data:  err.Error(),
				}
				return
			}
			content := response.Choices[0].Delta.Content

			m, _ := json.Marshal(struct {
				Content string `json:"content"`
			}{
				Content: content,
			})

			ResponseEventStream[msg.Uuid] <- EventStream{
				Event: "message",
				Data:  string(m),
			}
		}
	}()
	return
}
func ReceiveHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	u := uuid.New().String()
	_, ok := ResponseEventStream[u]
	if ok {
		fmt.Fprintf(w, "event: %v\ndata: %v\n\n", "error", "The UUID already exists; please refresh the page and attempt again.")
		w.(http.Flusher).Flush()
		return
	}
	ResponseEventStream[u] = make(chan EventStream)

	fmt.Fprintf(w, "event: %v\ndata: %v\n\n", "uuid", u)
	w.(http.Flusher).Flush()
	for {
		select {
		case msg := <-ResponseEventStream[u]:
			fmt.Fprintf(w, "event: %v\ndata: %v\n\n", msg.Event, msg.Data)
			w.(http.Flusher).Flush()
		case _ = <-r.Context().Done():
			delete(ResponseEventStream, u)
			return
		}
	}
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlTemplate))
}
