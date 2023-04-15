package main

import (
	"context"
	"io"
	"log"
	"os"
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
	_ "github.com/joho/godotenv/autoload"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/gridfs"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func mongoClient() *mongo.Client {
	serverAPIOptions := options.ServerAPI(options.ServerAPIVersion1)
	clientOptions := options.Client().ApplyURI(os.Getenv("MONGODB_SRV_RECORD")).SetServerAPIOptions(serverAPIOptions)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal(err)
	}

	// Check the connection
	err = client.Ping(context.TODO(), nil)
	if err != nil {
		log.Fatal(err)
	}

	return client
}

func main() {
	app := fiber.New()

	// Upload image to GridFS bucket in MongoDB
	app.Post("/api/image", func(c *fiber.Ctx) error {
		// Check if file is present in request body or not
		fileHeader, err := c.FormFile("file")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": true,
				"msg":   err.Error(),
			})
		}

		// Check if file is of type image or not
		fileExtension := regexp.MustCompile(`\.[a-zA-Z0-9]+$`).FindString(fileHeader.Filename)
		if fileExtension != ".jpg" && fileExtension != ".jpeg" && fileExtension != ".png" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": true,
				"msg":   "Invalid file type",
			})
		}

		// Read file content
		file, err := fileHeader.Open()
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": true,
				"msg":   err.Error(),
			})
		}
		content, err := io.ReadAll(file)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": true,
				"msg":   err.Error(),
			})
		}

		// Create db connection
		db := mongoClient().Database("go-fs")
		// Create bucket
		bucket, err := gridfs.NewBucket(db, options.GridFSBucket().SetName("images"))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": true,
				"msg":   err.Error(),
			})
		}

		// Upload file to GridFS bucket
		uploadStream, err := bucket.OpenUploadStream(fileHeader.Filename, options.GridFSUpload().SetMetadata(fiber.Map{"ext": fileExtension}))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": true,
				"msg":   err.Error(),
			})
		}

		// Close upload stream
		fieldId := uploadStream.FileID
		defer uploadStream.Close()

		// Write file content to upload stream
		fileSize, err := uploadStream.Write(content)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": true,
				"msg":   err.Error(),
			})
		}

		// Return response
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"error": false,
			"msg":   "Image uploaded successfully",
			"image": fiber.Map{
				"id":   fieldId,
				"name": fileHeader.Filename,
				"size": fileSize,
			},
		})
	})

	app.Listen(":3000")
}
