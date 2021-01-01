package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"

	"github.com/valyala/fasthttp"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type OEmbedResponse struct {
	Version string `json:"version"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Author  string `json:"author_name"`
}

type Response struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

var (
	shortenerCol *mongo.Collection
	collection   *mongo.Collection
	invisibleURL *mongo.Collection
	mongoContext = context.TODO()
	svc          *s3.S3
)

const (
	embedTemplate = `<html>
		<head>
			{{ if .Image }}
			<meta name="twitter:card" content="summary_large_image" />
			<meta property="og:image" content="{{.FileURL}}" />
			<meta property="og:description" content="{{.Desc}}" />
			{{ else }}
			<meta name="twitter:card" content="player" />
			<meta name="twitter:player" content="{{.FileURL}}">
			{{ end }}
			<meta name="theme-color" content="{{.Color}}" />
			<link type="application/json+oembed" href="{{.OEmbedURL}}" />
		</head>

		<body style="margin: 0px; background: #0e0e0e; height: 100%; display: flex; align-items: center">
			{{ if .Image }}
			<img width="500px" style="-webkit-user-select: none;margin: auto;" src="{{.FileURL}}" />
			{{ else }}
			<embed style="-webkit-user-select: none;margin: auto;" src="{{ .FileURL }}" />
			{{ end }}
		</body>
	</html>`

	showLinkTemplate = `<html>
		<head>
			<meta name="twitter:card" content="summary_large_image" />
			<meta property="og:image" content="{{.FileURL}}" />
		</head>

		<body style="margin: 0px; background: #0e0e0e; height: 100%; display: flex; align-items: center">
			<img width="500px" style="-webkit-user-select: none;margin: auto;" src="{{.FileURL}}" />
		</body>
	</html>`
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}

	connectToS3(os.Getenv("S3_ENDPOINT"))
	connectToDatabase(os.Getenv("MONGO_URI"))

	handler := fasthttp.CompressHandler(requestHandler)
	if err := fasthttp.ListenAndServe(":"+os.Getenv("PORT"), handler); err != nil {
		log.Fatal(err)
	}

	defer log.Printf("Listening to port %s", os.Getenv("PORT"))
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	requestPath := string(ctx.Path())
	basePath := path.Base(requestPath)
	host := string(ctx.Host())

	switch {
	case requestPath == "/":
		ctx.Redirect("https://astral.cool", 301)
	case strings.HasSuffix(basePath, ".json"):
		requestPath = strings.SplitN(basePath, ".json", 2)[0]
		var file bson.M
		if err := collection.FindOne(mongoContext, bson.M{"filename": requestPath}).Decode(&file); err != nil {
			sendErr(ctx, "invalid file")
			ctx.Done()
			return
		}

		embed := file["embed"].(primitive.M)
		embed["title"] = strings.ReplaceAll(embed["title"].(string), "{domain}", host)
		embed["author"] = strings.ReplaceAll(embed["author"].(string), "{domain}", host)

		ctx.Response.Header.SetCanonical([]byte("Content-Type"), []byte("application/json"))
		if err := json.NewEncoder(ctx).Encode(OEmbedResponse{
			Type:    "link",
			Version: "1.0",
			Title:   embed["title"].(string),
			Author:  embed["author"].(string),
		}); err != nil {
			log.Fatal(err)
		}
	case strings.HasPrefix(requestPath, "/s/") && basePath != "s":
		var shortened bson.M
		if err := shortenerCol.FindOne(mongoContext, bson.M{"shortId": basePath}).Decode(&shortened); err != nil {
			sendErr(ctx, "invalid short link")
			ctx.Done()
			return
		}

		destination := shortened["destination"].(string)
		if !strings.HasPrefix(destination, "http") {
			destination = "https://" + shortened["destination"].(string)
		}

		ctx.Redirect(destination, 301)
		ctx.Done()
	case basePath != "" && basePath != "favicon.ico":
		var file bson.M
		if strings.HasSuffix(basePath, "\u200B") {
			if err := invisibleURL.FindOne(mongoContext, bson.M{"_id": basePath}).Decode(&file); err != nil {
				sendErr(ctx, "no invisible url or file was found")
				ctx.Done()
				return
			}
			if file != nil {
				if err := collection.FindOne(mongoContext, bson.M{"filename": file["filename"]}).Decode(&file); err != nil {
					sendErr(ctx, "invalid file")
					ctx.Done()
					return
				}
			}
		} else {
			if err := collection.FindOne(mongoContext, bson.M{"filename": basePath}).Decode(&file); err != nil {
				sendErr(ctx, "invalid file")
				ctx.Done()
				return
			}
		}

		if file["userOnlyDomain"] == true && host != file["domain"].(string) {
			sendErr(ctx, "invalid file")
			ctx.Done()
			return
		}

		resp, err := svc.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(os.Getenv("S3_BUCKET_NAME")),
			Key:    aws.String(file["key"].(string)),
		})
		if err != nil {
			sendErr(ctx, err.Error())
			ctx.Done()
			return
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			sendErr(ctx, err.Error())
			ctx.Done()
			return
		}

		mimetype := strings.SplitN(file["mimetype"].(string), "/", 2)[0]
		cdnURL := "https://cdn.astral.cool" + "/" + file["key"].(string)
		embed := file["embed"].(primitive.M)

		embed["description"] = strings.ReplaceAll(embed["description"].(string), "{domain}", host)

		if embed["enabled"] == true {
			t, err := template.New("embed").Parse(embedTemplate)
			if err != nil {
				sendErr(ctx, err.Error())
				ctx.Done()
				return
			}

			data := struct {
				FileURL   string
				OEmbedURL string
				Desc      string
				Color     string
				Image     bool
			}{
				FileURL:   cdnURL,
				OEmbedURL: "https://" + host + "/" + file["filename"].(string) + ".json",
				Desc:      embed["description"].(string),
				Color:     embed["color"].(string),
				Image:     mimetype == "image",
			}

			ctx.SetContentType("text/html")
			err = t.Execute(ctx, data)
			if err != nil {
				sendErr(ctx, err.Error())
				ctx.Done()
			}
		} else if file["showLink"] == true {
			if mimetype == "video" {
				ctx.SetContentType(deref(resp.ContentType))
				ctx.SetBody(body)
				ctx.Done()
			} else {
				t, err := template.New("showLink").Parse(showLinkTemplate)
				if err != nil {
					sendErr(ctx, err.Error())
					ctx.Done()
					return
				}

				data := struct {
					FileURL string
				}{
					FileURL: cdnURL,
				}

				ctx.SetContentType("text/html")
				err = t.Execute(ctx, data)
				if err != nil {
					sendErr(ctx, err.Error())
					ctx.Done()
				}
			}
		} else {
			ctx.SetContentType(deref(resp.ContentType))
			ctx.SetBody(body)
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

func connectToS3(endpoint string) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-west-2"),
	})
	if err != nil {
		log.Fatal(err)
	}

	svc = s3.New(sess, &aws.Config{
		Endpoint: aws.String(endpoint),
	})

	defer fmt.Println("Connected to S3")
}

func connectToDatabase(mongoURL string) {
	client, err := mongo.Connect(mongoContext, options.Client().ApplyURI(mongoURL))
	if err != nil {
		log.Fatal(err)
	}

	database := client.Database("astral")
	collection = database.Collection("files")
	shortenerCol = database.Collection("shorteners")
	invisibleURL = database.Collection("invisibleurls")

	defer fmt.Println("Connected to MongoDB cluster")
}

func deref(str *string) string {
	if str != nil {
		return *str
	}

	return ""
}
