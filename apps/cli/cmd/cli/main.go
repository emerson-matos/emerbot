package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/orchestrator"
)

func main() {
	userID := flag.String("user", "local-user", "user id")
	text := flag.String("text", "", "message text")
	flag.Parse()

	if *text == "" {
		log.Fatal("use -text para enviar uma mensagem")
	}

	service := orchestrator.NewService(orchestrator.Config{})

	response, err := service.HandleMessage(context.Background(), domain.Message{
		UserID:    *userID,
		Text:      *text,
		Timestamp: time.Now().UTC(),
		MessageID: fmt.Sprintf("cli-%d", time.Now().UnixNano()),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(response.Text)
}
