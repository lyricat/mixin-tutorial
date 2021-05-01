package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/fox-one/mixin-sdk-go"
	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
)

var (
	client *mixin.Client
	// Specify the keystore file in the -config parameter
	config = flag.String("config", "", "keystore file path")
	pin    = flag.String("pin", "", "pin of keystore")
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

func handleTextMessage(ctx context.Context, msg *mixin.MessageView) error {
	// Decode the message content
	msgContent, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		return err
	}

	// Is it "cancel"? if so, reset the session
	if strings.ToUpper(string(msgContent)) == "CANCEL" {
		if err := askForSymbol(ctx, msg); err != nil {
			return respondError(ctx, msg, fmt.Errorf("Failed to ask for the asset symbol: %s", err))
		}
		return nil
	}

	// Try to get the user's session
	session := getSession(msg.UserID)
	if session == nil {
		// No existed session, this is a new user. Ask the user for the asset symbol.
		if err := askForSymbol(ctx, msg); err != nil {
			return respondError(ctx, msg, fmt.Errorf("Failed to ask for the asset symbol: %s", err))
		}
	} else {
		// There is an existed session
		if session.State == UserSessionStateInit {
			// If the user hasn't tell the bot which crypto them want, ask the user to transfer.
			if err := askForPayment(ctx, msg); err != nil {
				return respondError(ctx, msg, fmt.Errorf("Failed to ask for payment: %s", err))
			}
		} else {
			// tell user to complete the payment or be patient.
			if err := respondHint(ctx, msg, session); err != nil {
				return respondError(ctx, msg, fmt.Errorf("Failed to ask for payment: %s", err))
			}
		}
	}
	return nil
}

func handleTransfer(ctx context.Context, msg *mixin.MessageView) error {
	data, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		return err
	}

	// Decode the transfer view from message's content
	var view mixin.TransferView
	err = json.Unmarshal(data, &view)
	if err != nil {
		return err
	}

	session := getSession(msg.UserID)
	if session != nil && session.State == UserSessionStateSpecifiedSymbol {
		// has already specified an asset symbol in session
		// send a message and refund
		incomingAsset, err := client.ReadAsset(ctx, view.AssetID)
		if err != nil {
			return err
		}
		todo := fmt.Sprintf(
			"%s -> %s, swapping at 4swap...\nOops, I don't connect to 4swap yet.\nYour %s will be refunded.",
			incomingAsset.Symbol, session.Symbol, incomingAsset.Symbol,
		)
		respond(ctx, msg, mixin.MessageCategoryPlainText, []byte(todo), 1)
		// @TODO
		// swap the asset at 4swap.
		return transferBack(ctx, msg, &view, *pin)
	}
	// refund directly
	return transferBack(ctx, msg, &view, *pin)
}

func askForSymbol(ctx context.Context, msg *mixin.MessageView) error {
	// Set a session for this guy
	setSession(msg.UserID, &UserSession{
		State: UserSessionStateInit,
	})
	data := "Hi, which crypto do you want? Please reply the symbol (BTC, ETH, etc)"
	return respond(ctx, msg, mixin.MessageCategoryPlainText, []byte(data), 1)
}

func askForPayment(ctx context.Context, msg *mixin.MessageView) error {
	content, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		return err
	}

	// get the asset according to user's input
	asset, err := getAssetBySymbol(ctx, strings.TrimSpace(string(content)))
	if err != nil {
		return err
	}

	// move to next state
	setSession(msg.UserID, &UserSession{
		State:   UserSessionStateSpecifiedSymbol,
		Symbol:  asset.Symbol,
		AssetID: asset.AssetID,
	})

	// send the hint
	data := fmt.Sprintf("The price of %s (%s) is $%s, tap the \"swap\" link to continue.", asset.Symbol, asset.Name, asset.PriceUSD)
	if err := respond(ctx, msg, mixin.MessageCategoryPlainText, []byte(data), 1); err != nil {
		return err
	}

	// send the swap button
	buttons := fmt.Sprintf(`[{
    "label": "Swap to %s",
    "color": "#00BBFF",
    "action": "mixin://transfer/%s"
 	}]`, asset.Symbol, client.ClientID)
	return respond(ctx, msg, mixin.MessageCategoryAppButtonGroup, []byte(buttons), 2)
}

func respondHint(ctx context.Context, msg *mixin.MessageView, session *UserSession) error {
	msgTpl := `You choose to swap for %s, please transfer any crypto to the bot.
If you already transfered, please wait for a moment. It may cost time to swap.
If you want to cancel the swapping, please reply "cancel".`
	data := fmt.Sprintf(msgTpl, session.Symbol)
	return respond(ctx, msg, mixin.MessageCategoryPlainText, []byte(data), 1)
}

func respond(ctx context.Context, msg *mixin.MessageView, category string, data []byte, step int) error {
	payload := base64.StdEncoding.EncodeToString(data)
	id, _ := uuid.FromString(msg.MessageID)
	// Create a request
	reply := &mixin.MessageRequest{
		ConversationID: msg.ConversationID,
		RecipientID:    msg.UserID,
		MessageID:      uuid.NewV5(id, fmt.Sprintf("reply %d", step)).String(),
		Category:       category,
		Data:           payload,
	}
	// Send the response
	return client.SendMessage(ctx, reply)
}

func respondError(ctx context.Context, msg *mixin.MessageView, err error) error {
	respond(ctx, msg, mixin.MessageCategoryPlainText, []byte(fmt.Sprintln(err)), 1)
	return nil
}

func transferBack(ctx context.Context, msg *mixin.MessageView, view *mixin.TransferView, pin string) error {
	amount, err := decimal.NewFromString(view.Amount)
	if err != nil {
		return err
	}

	id, _ := uuid.FromString(msg.MessageID)

	input := &mixin.TransferInput{
		AssetID:    view.AssetID,
		OpponentID: msg.UserID,
		Amount:     amount,
		TraceID:    uuid.NewV5(id, "refund").String(),
		Memo:       "refund",
	}

	if _, err := client.Transfer(ctx, input, pin); err != nil {
		return err
	}
	return nil
}
