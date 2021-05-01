package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/fox-one/mixin-sdk-go"
)

type SwapAsset struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
}

type SwapAssetsRespData struct {
	Assets []SwapAsset `json:"assets"`
}

type SwapAssetsResp struct {
	Ts   int64              `json:"ts"`
	Data SwapAssetsRespData `json:"data"`
}

var (
	supportedAssets map[string]string
)

func initAssets() {
	// Gather assets from https://api.4swap.org/api/assets
	resp, err := http.Get("https://api.4swap.org/api/assets")
	if err != nil {
		log.Fatalln(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalln(err)
	}

	var result SwapAssetsResp
	json.Unmarshal(body, &result)

	supportedAssets = make(map[string]string)
	for i := 0; i < len(result.Data.Assets); i++ {
		asset := result.Data.Assets[i]
		supportedAssets[strings.ToUpper(asset.Symbol)] = asset.ID
	}

	log.Printf("Supported Assets: %d\n", len(supportedAssets))
	return
}

func getAssetBySymbol(ctx context.Context, symbol string) (*mixin.Asset, error) {
	symbol = strings.ToUpper(symbol)
	if assetID, found := supportedAssets[symbol]; found {
		return client.ReadAsset(ctx, assetID)
	}
	return nil, fmt.Errorf("Can't find asset (%s)", symbol)
}
