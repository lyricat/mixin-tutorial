package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fox-one/mixin-sdk-go"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

var (
	// to store your BTC snapshots
	snapshots []*mixin.Snapshot
	// the access token
	accessToken string
	// loading state
	loading bool
)

const BTCAssetID = "c6d0c728-2624-429b-8e0d-d9d19b6592fa"

func StartHttpServer() {
	// define routers
	// I use go-chi here, which is a lightweight http router for golang
	// https://github.com/go-chi/chi
	{
		mux := chi.NewMux()
		mux.Use(middleware.Recoverer)
		mux.Use(middleware.StripSlashes)
		mux.Use(cors.AllowAll().Handler)
		mux.Use(middleware.Logger)

		// health check
		mux.Get("/hc", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ok"))
		})

		// render index page
		mux.Get("/", renderIndexPage)

		// handle oauth
		mux.Handle("/oauth", HandleOauth(client.ClientID, *clientSecret))

		// handle api
		mux.Mount("/api", HandleRest())

		// launch the http server at port 8080
		go http.ListenAndServe(":8080", mux)
	}
}

func renderIndexPage(w http.ResponseWriter, r *http.Request) {
	// read the template from index.html
	t, _ := template.ParseFiles("index.html")
	type IndexPageParams struct {
		Signed   bool
		Loading  bool
		ClientID string
	}
	// pass parameters into the template and render it
	t.Execute(w, IndexPageParams{
		Signed:   accessToken != "",
		Loading:  loading,
		ClientID: client.ClientID,
	})
}

func HandleOauth(clientID, clientSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// because we set 'http://localhost:8080/oauth' as the callback url at Developer Dashboard
		// Mixin's OAuth will redirect each successful OAuth request to the callback url
		// with a `code`, which i will use it to exchange for the access token.
		ctx := r.Context()
		var scope string
		var err error

		// exchange the access token with the code.
		accessToken, scope, err = mixin.AuthorizeToken(ctx, clientID, clientSecret, r.URL.Query().Get("code"), "")
		if err != nil {
			renderError(w, err, 401)
			return
		}

		// check the scopes I needed.
		if !strings.Contains(scope, "ASSETS:READ") || !strings.Contains(scope, "SNAPSHOTS:READ") || !strings.Contains(scope, "PROFILE:READ") {
			renderError(w, fmt.Errorf("Incorrect scope"), 400)
			return
		}

		// try to use the access token to get user's information
		user, err := mixin.UserMe(ctx, accessToken)
		if err != nil {
			renderError(w, err, 500)
			return
		}

		// You may wanna save the user and access token to database
		log.Println(user, accessToken)

		// fetch all BTC snapshots from mixin network
		go fetchSnapshots()

		// redirect to the index page
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
	}
}

func HandleRest() http.Handler {
	r := chi.NewRouter()

	r.Route("/assets", func(r chi.Router) {
		// two APIs, one for the BTC balance, another for the BTC snapshots
		r.Get("/balance", getBalance)
		r.Get("/snapshots", getSnapshots)
	})

	return r
}

func fetchSnapshots() {
	var offset time.Time
	var err error
	var incoming []*mixin.Snapshot
	const LIMIT = 500

	loading = true
	ctx := context.TODO()

	// load snapshots from the Mixin Network.
	// the SDK's method ReadSnapshots is compatible with API https://developers.mixin.one/document/wallet/api/snapshots
	incoming, err = mixin.ReadSnapshots(ctx, accessToken, BTCAssetID, offset, "ASC", LIMIT)
	if err != nil {
		log.Panic(err)
		return
	}
	snapshots = append(snapshots, incoming...)

	// continue fetch the snapshots with increased offset until i get all snapshots
	for len(incoming) == LIMIT {
		offset = incoming[len(incoming)-1].CreatedAt
		incoming, err = mixin.ReadSnapshots(ctx, accessToken, BTCAssetID, offset, "ASC", LIMIT)
		log.Printf("load snapshots %d, %v", len(incoming), offset)
		if err != nil {
			log.Panic(err)
			return
		}
		snapshots = append(snapshots, incoming...)
		time.Sleep(time.Second)
	}
	loading = false
}

func getBalance(w http.ResponseWriter, r *http.Request) {
	// unlike the `getSnapshots` function, I don't store the responese from Mixin Network here
	// because this API call has a simple responese so I'll forward to the browser directly.
	ctx := r.Context()
	asset, err := mixin.ReadAsset(ctx, accessToken, BTCAssetID)
	if err != nil {
		renderError(w, err, 500)
	}
	renderJSON(w, asset)
}

func getSnapshots(w http.ResponseWriter, r *http.Request) {
	// because fetch all snapshots costs time,
	// this API responds with the snapshots stored in the memory.
	renderJSON(w, snapshots)
}

// some helper functions to render json and error
func renderJSON(w http.ResponseWriter, object interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(object); err != nil {
		renderError(w, fmt.Errorf("Unknown error"), 500)
	}
}

func renderError(w http.ResponseWriter, err error, code int) {
	http.Error(w, err.Error(), code)
}
