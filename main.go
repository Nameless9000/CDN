package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

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

type Response struct {
	Success bool
	Error   string
}

var (
	collection   *mongo.Collection
	invisibleURL *mongo.Collection
	mongoContext = context.TODO()
	svc          *s3.S3
)

const (
	embedTemplate = `<html>
		<head>
			<meta property="og:image" content="%s" />
			<meta property="og:title" content="%s" />
			<meta property="og:description" content="%s" />
			<meta name="theme-color" content="%s" />
			<meta name="twitter:card" content="summary_large_image" />
		</head>

		<h1>Image uploaded by %s on %s.</h1>
		<img src="%s" />
	</html>`

	showLinkTemplate = `<html>
		<head>
			<meta property="og:image" content="%s" />
			<meta name="twitter:card" content="summary_large_image" />
		</head>

		<h1>Image uploaded by %s on %s.</h1>
		<img src="%s" />
	</html>`
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
		var invisURL bson.M
		if err := invisibleURL.FindOne(mongoContext, bson.M{"_id": path}).Decode(&invisURL); err != nil {
			if err := collection.FindOne(mongoContext, bson.M{"filename": path}).Decode(&file); err != nil {
				sendErr(ctx, "no invisible url or file was found")
				ctx.Done()
				return
			}
		}
		if invisURL != nil {
			if err := collection.FindOne(mongoContext, bson.M{"filename": invisURL["filename"]}).Decode(&file); err != nil {
				sendErr(ctx, "invalid file")
				ctx.Done()
				return
			}
		}

		mimetype := strings.SplitN(file["mimetype"].(string), "/", 2)[0]

		uploaderID := file["uploader"].(primitive.M)["uid"].(string)
		uploaderUsername := file["uploader"].(primitive.M)["username"].(string)
		resp, err := svc.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(os.Getenv("S3_BUCKET_NAME")),
			Key:    aws.String(uploaderID + "/" + file["filename"].(string)),
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

		if mimetype == "video" {
			ctx.SetContentType(deref(resp.ContentType))
			ctx.SetBody(body)
			ctx.Done()
		} else if mimetype == "image" {
			imageURL := "https://cdn.astral.cool/" + uploaderID + "/" + file["filename"].(string)
			if file["displayType"] == "embed" {
				ctx.SetContentType("text/html")

				title := file["filename"].(string)
				if file["embed"].(primitive.M)["title"] != "default" {
					title = file["embed"].(primitive.M)["title"].(string)
				}
				description := "Uploaded by " + uploaderUsername + " on " + file["dateUploaded"].(string)
				if file["embed"].(primitive.M)["description"] != "default" {
					description = file["embed"].(primitive.M)["description"].(string)
				}
				color := file["embed"].(primitive.M)["color"]

				formatted := fmt.Sprintf(embedTemplate, imageURL, title, description, color, uploaderUsername, file["dateUploaded"], imageURL)
				fmt.Fprintln(ctx, formatted)
			} else if file["showLink"] == true {
				ctx.SetContentType("text/html")

				formatted := fmt.Sprintf(showLinkTemplate, imageURL, uploaderUsername, file["dateUploaded"], imageURL)
				fmt.Fprintln(ctx, formatted)
			} else {
				ctx.SetContentType(deref(resp.ContentType))
				ctx.SetBody(body)
				ctx.Done()
			}
		} else {
			sendErr(ctx, "invalid mimetype")
			ctx.Done()
		}
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
	invisibleURL = client.Database("astral").Collection("invisibleurls")

	defer fmt.Println("connected to database")
}

func deref(str *string) string {
	if str != nil {
		return *str
	}

	return ""
}
