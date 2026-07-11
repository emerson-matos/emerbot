package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/llm"
	"github.com/emerson/emerbot/packages/memory"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/tools"
)

func main() {
	userID := flag.String("user", "local-user", "user id")
	text := flag.String("text", "", "message text")
	flag.Parse()

	if *text == "" {
		log.Fatal("use -text para enviar uma mensagem")
	}

	stores := memory.NewInMemoryStores()
	_ = stores.Save(context.Background(), domain.Memory{
		UserID: *userID,
		Type:   "Goal",
		ID:     "LearnAWS",
		Value:  "Construir um assistente via WhatsApp gastando menos de R$20 por mes.",
	})

	service := orchestrator.NewService(
		llm.StaticClient{},
		stores,
		stores,
		tools.NewRegistry(tools.EchoTool{}),
	)

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

