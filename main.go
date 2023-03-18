package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/sashabaranov/go-openai"
	"html"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
)

var Ai = openai.NewClient("your token")

type EventStream struct {
	Event string
	Data  string
}
type DialogMsg = map[string]chan EventStream

var ResponseEventStream = make(DialogMsg)

func main() {

	http.HandleFunc("/", IndexHandler)
	http.HandleFunc("/send", SendMsgHandler)
	http.HandleFunc("/receive", ReceiveHandler)
	fmt.Println("start service on 8088")
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
		MaxTokens: 1000,
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
			ResponseEventStream[msg.Uuid] <- EventStream{
				Event: "message",
				Data:  content,
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
			fmt.Fprintf(w, "event: %v\ndata: %v\n\n", msg.Event, parseString(html.EscapeString(msg.Data)))
			w.(http.Flusher).Flush()
		case _ = <-r.Context().Done():
			delete(ResponseEventStream, u)
			return
		}
	}
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	data, err := ioutil.ReadFile("index.html")
	if err != nil {
		log.Fatal(err)
	}
	w.Write(data)
}

func GenerateUUID() (string, error) {
	uuid := make([]byte, 16)
	_, err := rand.Read(uuid)
	if err != nil {
		return "", fmt.Errorf("failed to generate UUID: %!(NOVERB)v", err)
	}
	// UUID version 4 variant bits
	uuid[8] = uuid[8]&^0xc0 | 0x80
	uuid[6] = uuid[6]&^0xf0 | 0x40
	uuidString := fmt.Sprintf("%!(NOVERB)x-%!(NOVERB)x-%!(NOVERB)x-%!(NOVERB)x-%!(NOVERB)x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
	return uuidString, nil
}

func parseString(input string) string {

	//fmt.Println("==============")
	//s := regexp.MustCompile("```(\\w+)").ReplaceAllString(input, "<code lang='$1'>")
	//s = regexp.MustCompile("```\\n").ReplaceAllString(s, "</code></br>")
	// 替换 "\n" 为 "</br>"
	s := strings.ReplaceAll(input, "\n", "</br>")
	// 替换 "\t" 为 "&nbsp;&nbsp;&nbsp;&nbsp;"
	s = strings.ReplaceAll(s, "\t", "&nbsp;&nbsp;&nbsp;&nbsp;")
	// 替换空格为 "&nbsp;"
	s = strings.ReplaceAll(s, " ", "&nbsp;")

	return s
}
