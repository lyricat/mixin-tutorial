package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"github.com/fox-one/mixin-sdk-go"
	"github.com/gofrs/uuid"
)

var (
	client *mixin.Client
	// Specify the keystore file in the -config parameter
	config       = flag.String("config", "", "keystore file path")
	pin          = flag.String("pin", "", "pin of keystore")
	clientSecret = flag.String("clientSecret", "", "client secret")
)

func main() {
	var err error
	// Use flag package to parse the parameters
	flag.Parse()

	// Open the keystore file
	f, err := os.Open(*config)
	if err != nil {
		log.Panicln(err)
	}

	// Read the keystore file as json into mixin.Keystore, which is a go struct
	var store mixin.Keystore
	if err := json.NewDecoder(f).Decode(&store); err != nil {
		log.Panicln(err)
	}

	// Create a Mixin Client from the keystore, which is the instance to invoke Mixin APIs
	client, err = mixin.NewFromKeystore(&store)
	if err != nil {
		log.Panicln(err)
	}

	// Get supported assets from 4swap
	initAssets()

	// Prepare the message loop that handle every incoming messages,
	// We use a callback function to handle them.
	h := func(ctx context.Context, msg *mixin.MessageView, userID string) error {
		// if there is no valid user id in the message, drop it
		if userID, _ := uuid.FromString(msg.UserID); userID == uuid.Nil {
			return nil
		}

		if msg.Category == mixin.MessageCategorySystemAccountSnapshot {
			// if the message is a transfer message
			// and it is sent by other users, then handle it
			if msg.UserID != client.ClientID {
				return handleTransfer(ctx, msg)
			}
			// or just drop it
			return nil
		} else if msg.Category == mixin.MessageCategoryPlainText {
			// if the message is a text message
			// then handle the message
			return handleTextMessage(ctx, msg)
		} else {
			return askForSymbol(ctx, msg)
		}
	}

	ctx := context.Background()

	// Start httpd
	StartHttpServer()

	// Start the message loop.
	for {
		// Pass the callback function into the `BlazeListenFunc`
		if err := client.LoopBlaze(ctx, mixin.BlazeListenFunc(h)); err != nil {
			log.Printf("LoopBlaze: %v", err)
		}

		// Sleep for a while
		time.Sleep(time.Second)
	}
}
