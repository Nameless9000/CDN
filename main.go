package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

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

type OEmbedResponse struct {
	Version string `json:"version"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Author  string `json:"author_name"`
}

var (
	collection   *mongo.Collection
	invisibleURL *mongo.Collection
	mongoContext = context.TODO()
	svc          *s3.S3
)

const (
	embedTemplate = `<html style="background: #181818;">
		<head>
			{{ if .Image }}
			<meta name="twitter:card" content="summary_large_image" />
			<meta property="og:image" content="{{.FileURL}}" />
			<meta property="og:description" content="{{.Desc}}" />
			{{ else }}
			<meta name="twitter:card" content="player" />
			<meta name="twitter:player" content="{{ .FileURL }}">
			{{ end }}
			<meta name="theme-color" content="{{.Color}}" />
			<link type="application/json+oembed" href="{{.OEmbedURL}}" />
		</head>

		<div style="min-height: 95vh; padding: 0 0.5rem; display: flex; flex-direction: column; justify-content: center; align-items: center; text-align: center;">
			<h1 style="font-family: Arial, Helvetica, sans-serif; color: white; font-size: 20px; font-weight: 400">File uploaded by {{.Uploader}} on {{.Date}}.</h1>
			{{ if .Image }}
			<img width="500px" style="padding: 10px" src="{{.FileURL}}" />
			{{ else }}
			<embed src="{{ .FileURL }}" />
			{{ end }}
		</div>
	</html>`

	showLinkTemplate = `<html style="background: #181818;">
		<head>
			<meta property="og:image" content="{{.FileURL}}" />
			<meta name="twitter:card" content="summary_large_image" />
		</head>

		<div style="min-height: 95vh; padding: 0 0.5rem; display: flex; flex-direction: column; justify-content: center; align-items: center; text-align: center;">
			<h1 style="font-family: Arial, Helvetica, sans-serif; color: white; font-size: 20px; font-weight: 400">File uploaded by {{.Uploader}} on {{.Date}}.</h1>
			<img width="500px" style="padding: 10px" src="{{.FileURL}}" />
		</div>
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
	case strings.HasPrefix(path, "/oembed/"):
		path = strings.SplitN(path[1:], "/", 2)[1]
		var file bson.M
		if err := collection.FindOne(mongoContext, bson.M{"filename": path}).Decode(&file); err != nil {
			sendErr(ctx, "invalid file")
			ctx.Done()
			return
		}

		embedTitle := file["embed"].(primitive.M)["title"].(string)
		uploaderUsername := file["uploader"].(primitive.M)["username"].(string)
		dateUploaded := file["dateUploaded"].(string)
		fileName := file["filename"].(string)

		fileSize := ""
		if file["size"] != nil {
			fileSize = file["size"].(string)
		}

		domain := ""
		if file["domain"] != nil {
			domain = file["domain"].(string)
		}

		author := ""
		if file["embed"].(primitive.M)["author"] == true {
			author = uploaderUsername
		}

		title := file["filename"].(string)
		if file["embed"].(primitive.M)["title"] != "default" {
			title = strings.ReplaceAll(embedTitle, "{username}", uploaderUsername)
			title = strings.ReplaceAll(title, "{date}", dateUploaded)
			title = strings.ReplaceAll(title, "{file}", fileName)
			title = strings.ReplaceAll(title, "{size}", fileSize)
			title = strings.ReplaceAll(title, "{domain}", domain)
		}

		ctx.Response.Header.SetCanonical([]byte("Content-Type"), []byte("application/json"))
		if err := json.NewEncoder(ctx).Encode(OEmbedResponse{
			Type:    "link",
			Version: "1.0",
			Title:   title,
			Author:  author,
		}); err != nil {
			log.Fatal(err)
		}
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

		fileURL := "https://cdn.astral.cool/" + uploaderID + "/" + file["filename"].(string)
		if file["displayType"] == "embed" {
			ctx.SetContentType("text/html")

			embedDescription := file["embed"].(primitive.M)["description"].(string)
			dateUploaded := file["dateUploaded"].(string)
			fileName := file["filename"].(string)

			fileSize := ""
			if file["size"] != nil {
				fileSize = file["size"].(string)
			}

			domain := ""
			if file["domain"] != nil {
				domain = file["domain"].(string)
			}

			description := "Uploaded by " + uploaderUsername + " on " + dateUploaded
			if file["embed"].(primitive.M)["description"] != "default" {
				description = strings.ReplaceAll(embedDescription, "{username}", uploaderUsername)
				description = strings.ReplaceAll(description, "{date}", dateUploaded)
				description = strings.ReplaceAll(description, "{file}", fileName)
				description = strings.ReplaceAll(description, "{size}", fileSize)
				description = strings.ReplaceAll(description, "{domain}", domain)
			}
			color := file["embed"].(primitive.M)["color"].(string)
			if file["embed"].(primitive.M)["randomColor"] == true {
				color = generateColor()
			}

			t, err := template.New("embed").Parse(embedTemplate)
			if err != nil {
				sendErr(ctx, "something went wrong")
				ctx.Done()
				return
			}

			data := struct {
				FileURL   string
				OEmbedURL string
				Desc      string
				Color     string
				Uploader  string
				Date      string
				Image     bool
			}{
				FileURL:   fileURL,
				OEmbedURL: os.Getenv("CDN_URL") + "/oembed/" + file["filename"].(string),
				Desc:      description,
				Color:     color,
				Uploader:  uploaderUsername,
				Date:      dateUploaded,
				Image:     mimetype == "image",
			}

			err = t.Execute(ctx, data)
			if err != nil {
				sendErr(ctx, "something went wrong")
				ctx.Done()
			}
		} else if file["showLink"] == true {
			if mimetype == "video" {
				ctx.SetContentType(deref(resp.ContentType))
				ctx.SetBody(body)
				ctx.Done()
			} else if mimetype == "image" {
				ctx.SetContentType("text/html")

				dateUploaded := file["dateUploaded"].(string)

				t, err := template.New("showlink").Parse(showLinkTemplate)
				if err != nil {
					sendErr(ctx, "something went wrong")
					ctx.Done()
					return
				}

				data := struct {
					FileURL  string
					Uploader string
					Date     string
				}{
					FileURL:  fileURL,
					Uploader: uploaderUsername,
					Date:     dateUploaded,
				}

				err = t.Execute(ctx, data)
				if err != nil {
					sendErr(ctx, "something went wrong")
					ctx.Done()
				}
			} else {
				sendErr(ctx, "invalid mimetype")
				ctx.Done()
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

func generateColor() string {
	rand.Seed(time.Now().UnixNano())
	Blue := rand.Intn(255)
	Green := rand.Intn(255)
	Red := rand.Intn(255)
	return "#" + fmt.Sprintf("%x", Red) + fmt.Sprintf("%x", Green) + fmt.Sprintf("%x", Blue)
}
