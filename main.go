package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	_ "github.com/joho/godotenv/autoload"			// Load .env file automatically
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/gridfs"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Create MongoDB client connection
// @return *mongo.Client client
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

// Set response headers according to file extension
// @param c *fiber.Ctx context
// @param buff bytes.Buffer
// @param ext string
// @return error error
func setResponseHeaders(c *fiber.Ctx, buff bytes.Buffer, ext string) error {
	switch ext {
	case ".png":
		c.Set("Content-Type", "image/png")
	case ".jpg":
		c.Set("Content-Type", "image/jpeg")
	case ".jpeg":
		c.Set("Content-Type", "image/jpeg")
	}

	c.Set("Cache-Control", "public, max-age=31536000")
	c.Set("Content-Length", strconv.Itoa(len(buff.Bytes())))

	return c.Next()
}

func main() {
	// Create new Fiber app instance with default config settings
	app := fiber.New()

	// Upload image to GridFS bucket in MongoDB
	// @param file file
	// @return image metadata
	app.Post("/api/image", func(c *fiber.Ctx) error {
		// Check if file is present in request body or not
		fileHeader, err := c.FormFile("image")
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

		// Close upload stream after uploading file
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

	// Get image from GridFS bucket in MongoDB using image id
	// @param id string
	// @return image content
	app.Get("/api/image/id/:id", func(c *fiber.Ctx) error {
		// Get image id from request params and convert it to ObjectID
		id, err := primitive.ObjectIDFromHex(c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": true,
				"msg":   err.Error(),
			})
		}

		// Create db connection
		db := mongoClient().Database("go-fs")

		// Create variable to store image metadata
		var avatarMetadata bson.M

		// Get image metadata from GridFS bucket
		if err := db.Collection("images.files").FindOne(c.Context(), fiber.Map{"_id": id}).Decode(&avatarMetadata); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": true,
				"msg":   "Avatar not found",
			})
		}

		// Create buffer to store image content
		var buffer bytes.Buffer
		// Create bucket
		bucket, _ := gridfs.NewBucket(db, options.GridFSBucket().SetName("images"))
		// Download image from GridFS bucket to buffer
		bucket.DownloadToStream(id, &buffer)

		// Set required headers
		setResponseHeaders(c, buffer, avatarMetadata["metadata"].(bson.M)["ext"].(string))

		// Return image
		return c.Send(buffer.Bytes())
	})

	// Get image from GridFS bucket in MongoDB using image name
	// @param name string
	// @return image content
	app.Get("/api/image/name/:name", func(c *fiber.Ctx) error {
		// Get image name from request params
		name := c.Params("name")

		// Create db connection
		db := mongoClient().Database("go-fs")

		// Create variable to store image metadata
		var avatarMetadata bson.M

		// Get image metadata from GridFS bucket
		if err := db.Collection("images.files").FindOne(c.Context(), fiber.Map{"filename": name}).Decode(&avatarMetadata); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": true,
				"msg":   "Avatar not found",
			})
		}

		// Create buffer to store image content
		var buffer bytes.Buffer
		// Create bucket
		bucket, _ := gridfs.NewBucket(db, options.GridFSBucket().SetName("images"))
		// Download image from GridFS bucket to buffer
		bucket.DownloadToStreamByName(name, &buffer)

		// Set required headers
		setResponseHeaders(c, buffer, avatarMetadata["metadata"].(bson.M)["ext"].(string))

		// Return image
		return c.Send(buffer.Bytes())
	})

	app.Listen(":3000")
}
