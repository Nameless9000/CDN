// package main

// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"log"
// 	"os"
// 	"time"

// 	"github.com/valyala/fasthttp"
// 	"go.mongodb.org/mongo-driver/bson"
// 	"go.mongodb.org/mongo-driver/mongo"
// 	"go.mongodb.org/mongo-driver/mongo/options"

// 	"github.com/joho/godotenv"
// )

// var (
// 	database:
// )

// // Response The response we send back as json.
// type Response struct {
// 	Success bool
// 	Error   string
// }

// func main() {
// 	err := godotenv.Load()
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	port := os.Getenv("PORT")

// 	handler := fasthttp.CompressHandler(requestHandler)

// 	if err := fasthttp.ListenAndServe(":"+port, handler); err != nil {
// 		log.Fatal(err)
// 	}

// 	log.Printf("Listening to port %s", port)
// }

// func requestHandler(ctx *fasthttp.RequestCtx) {
// 	path := string(ctx.Path())

// 	switch {
// 	case path == "/":
// 		ctx.Redirect("https://astral.cool", 301)
// 	case path != "/":

// 	}
// }

// func sendErr(ctx *fasthttp.RequestCtx, errMsg string) {
// 	ctx.Response.Header.SetCanonical([]byte("Content-Type"), []byte("application/json"))
// 	if err := json.NewEncoder(ctx).Encode(Response{Success: false, Error: errMsg}); err != nil {
// 		log.Fatal(err)
// 	}
// }

// func connectToDatabase() {
// 	mongoURL := os.Getenv("MONGO_URI")

// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()
// 	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURL))
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer client.Disconnect(ctx)
// 	collection := client.Database("astral").Collection("users")

// 	var user bson.M
// 	if err = collection.FindOne(ctx, bson.M{"username": "aspect"}).Decode(&user); err != nil {
// 		log.Fatal(err)
// 	}
// 	fmt.Println(user["username"], "has uploaded", user["uploads"], "files.")
// }

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/valyala/fasthttp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/joho/godotenv"
)

var (
	collection   *mongo.Collection
	mongoContext = context.TODO()
)

// Response The response we send back as json.
type Response struct {
	Success bool
	Error   string
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}

	port := os.Getenv("PORT")
	mongoURL := os.Getenv("MONGO_URI")

	connectToDatabase(mongoURL)

	handler := fasthttp.CompressHandler(requestHandler)

	if err := fasthttp.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}

	log.Printf("Listening to port %s", port)
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())

	switch {
	case path == "/":
		ctx.Redirect("https://astral.cool", 301)
	case path != "/" && path != "favicon.ico":
		path = path[1:]
		var file bson.M
		if err := collection.FindOne(mongoContext, bson.M{"filename": path}).Decode(&file); err != nil {
			sendErr(ctx, "Invalid File")
			ctx.Done()
			return
		}
		fmt.Fprintln(ctx, file)
	}
}

func sendErr(ctx *fasthttp.RequestCtx, errMsg string) {
	ctx.Response.Header.SetCanonical([]byte("Content-Type"), []byte("application/json"))
	if err := json.NewEncoder(ctx).Encode(Response{Success: false, Error: errMsg}); err != nil {
		log.Fatal(err)
	}
}

func connectToDatabase(mongoURL string) {
	client, err := mongo.Connect(mongoContext, options.Client().ApplyURI(mongoURL))
	if err != nil {
		log.Fatal(err)
	}
	collection = client.Database("astral").Collection("files")

	defer fmt.Println("connected to database")
}
