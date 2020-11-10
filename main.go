package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/aws/session"

	"github.com/valyala/fasthttp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/joho/godotenv"
)

// Response The response we send back as json.
type Response struct {
	Success bool
	Error   string
}

var (
	collection   *mongo.Collection
	mongoContext = context.TODO()
	svc          *s3.S3
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}

	port := os.Getenv("PORT")
	mongoURL := os.Getenv("MONGO_URI")

	connectToDatabase(mongoURL)
	connectToS3()

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
			sendErr(ctx, "invalid file")
			ctx.Done()
			return
		}

		uploaderID := file["uploader"].(primitive.M)["uid"]
		resp, err := svc.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(os.Getenv("S3_BUCKET_NAME")),
			Key:    aws.String(uploaderID.(string) + "/" + path),
		})
		if err != nil {
			sendErr(ctx, err.Error())
			ctx.Done()
			return
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			sendErr(ctx, "something went wrong")
			ctx.Done()
			return
		}

		ctx.SetBody(body)
		ctx.Done()
	}
}

func sendErr(ctx *fasthttp.RequestCtx, errMsg string) {
	ctx.Response.Header.SetCanonical([]byte("Content-Type"), []byte("application/json"))
	if err := json.NewEncoder(ctx).Encode(Response{Success: false, Error: errMsg}); err != nil {
		log.Fatal(err)
	}
}

func connectToS3() {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-west-2"),
	})
	if err != nil {
		log.Fatal(err)
	}

	svc = s3.New(sess, &aws.Config{
		Endpoint: aws.String(os.Getenv("S3_ENDPOINT")),
	})
}

func connectToDatabase(mongoURL string) {
	client, err := mongo.Connect(mongoContext, options.Client().ApplyURI(mongoURL))
	if err != nil {
		log.Fatal(err)
	}
	collection = client.Database("astral").Collection("files")

	defer fmt.Println("connected to database")
}
