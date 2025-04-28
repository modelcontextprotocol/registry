package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDB is an implementation of the Database interface using MongoDB
type MongoDB struct {
	client     *mongo.Client
	database   *mongo.Database
	collection *mongo.Collection
}

// NewMongoDB creates a new instance of the MongoDB database
func NewMongoDB(ctx context.Context, connectionURI, databaseName, collectionName string) (*MongoDB, error) {
	// Set client options and connect to MongoDB
	clientOptions := options.Client().ApplyURI(connectionURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	// Ping the MongoDB server to verify the connection
	if err = client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	// Get database and collection
	database := client.Database(databaseName)
	collection := database.Collection(collectionName)

	// Create indexes for better query performance
	models := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "name", Value: 1}},
		},
		{
			Keys:    bson.D{{Key: "id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		// add an index for the combination of name and version
		{
			Keys:    bson.D{{Key: "name", Value: 1}, {Key: "versiondetail.version", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	}

	_, err = collection.Indexes().CreateMany(ctx, models)
	if err != nil {
		return nil, err
	}

	return &MongoDB{
		client:     client,
		database:   database,
		collection: collection,
	}, nil
}

// List retrieves MCPRegistry entries with optional filtering and pagination
func (db *MongoDB) List(ctx context.Context, filter map[string]interface{}, cursor string, limit int) ([]*model.Entry, string, error) {
	if limit <= 0 {
		// Set default limit if not provided
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Convert Go map to MongoDB filter
	mongoFilter := bson.M{
		"versiondetail.islatest": true,
	}
	// Map common filter keys to MongoDB document paths
	for k, v := range filter {
		// Handle nested fields with dot notation
		switch k {
		case "publisher.trusted":
			mongoFilter["publisher.trusted"] = v
		default:
			mongoFilter[k] = v
		}
	}

	// Setup pagination options
	findOptions := options.Find()

	// If cursor is provided, add condition to filter to only get records after the cursor
	if cursor != "" {
		// Validate that the cursor is a valid UUID
		if _, err := uuid.Parse(cursor); err != nil {
			return nil, "", fmt.Errorf("invalid cursor format: %w", err)
		}

		// Fetch the document at the cursor to get its sort values
		var cursorDoc model.Entry
		err := db.collection.FindOne(ctx, bson.M{"id": cursor}).Decode(&cursorDoc)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				// If cursor document not found, start from beginning
			} else {
				return nil, "", err
			}
		} else {
			// Use the cursor document's ID to paginate (records with ID > cursor's ID)
			mongoFilter["id"] = bson.M{"$gt": cursor}
		}
	}

	// Set sort order by ID (for consistent pagination)
	findOptions.SetSort(bson.M{"id": 1})

	// Set limit if provided and valid
	if limit > 0 {
		findOptions.SetLimit(int64(limit))
	}

	// Execute find operation with options
	mongoCursor, err := db.collection.Find(ctx, mongoFilter, findOptions)
	if err != nil {
		return nil, "", err
	}
	defer mongoCursor.Close(ctx)

	// Decode results
	var results []*model.Entry
	if err = mongoCursor.All(ctx, &results); err != nil {
		return nil, "", err
	}

	// Determine the next cursor
	nextCursor := ""
	if len(results) > 0 && limit > 0 && len(results) >= limit {
		// Use the last item's ID as the next cursor
		nextCursor = results[len(results)-1].ID
	}

	return results, nextCursor, nil
}

// GetByID retrieves a single ServerDetail by its ID
func (db *MongoDB) GetByID(ctx context.Context, id string) (*model.ServerDetail, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Create a filter for the ID
	filter := bson.M{"id": id}

	// Find the entry in the database
	var entry model.ServerDetail
	err := db.collection.FindOne(ctx, filter).Decode(&entry)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("error retrieving entry: %w", err)
	}

	// Create and return a ServerDetail from the entry data
	return &entry, nil
}

// Publish adds a new ServerDetail to the database
func (db *MongoDB) Publish(ctx context.Context, serverDetail *model.ServerDetail) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// find a server detail with the same name and check that the current version is greater than the existing one
	filter := bson.M{
		"name":                   serverDetail.Name,
		"versiondetail.islatest": true,
	}

	var existingEntry model.ServerDetail
	err := db.collection.FindOne(ctx, filter).Decode(&existingEntry)
	if err != nil && err != mongo.ErrNoDocuments {
		return fmt.Errorf("error checking existing entry: %w", err)
	}

	// check that the current version is greater than the existing one
	if serverDetail.VersionDetail.Version <= existingEntry.VersionDetail.Version {
		return fmt.Errorf("version must be greater than existing version")
	}

	serverDetail.ID = uuid.New().String()
	serverDetail.VersionDetail.IsLatest = true
	serverDetail.VersionDetail.ReleaseDate = time.Now().Format(time.RFC3339)

	// Insert the entry into the database
	_, err = db.collection.InsertOne(ctx, serverDetail)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("error inserting entry: %w", err)
	}

	// update the existing entry to not be the latest version
	if existingEntry.ID != "" {
		_, err = db.collection.UpdateOne(ctx, bson.M{"id": existingEntry.ID}, bson.M{"$set": bson.M{"versiondetail.islatest": false}})
		if err != nil {
			return fmt.Errorf("error updating existing entry: %w", err)
		}

	}

	return nil
}

// Close closes the database connection
func (db *MongoDB) Close() error {
	return db.client.Disconnect(context.Background())
}

// Connection returns information about the database connection
func (db *MongoDB) Connection() *ConnectionInfo {
	isConnected := false
	// Check if the client is connected
	if db.client != nil {
		// A quick ping with 1 second timeout to verify connection
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := db.client.Ping(ctx, nil)
		isConnected = (err == nil)
	}

	return &ConnectionInfo{
		Type:        ConnectionTypeMongoDB,
		IsConnected: isConnected,
		Raw:         db.client,
	}
}
