package graph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"codergag/internal/models"
)

// MongoGraphRepository implements GraphRepository on top of MongoDB.
// Nodes and edges are stored in separate collections with full BSON documents,
// enabling rich indexing and flexible querying.
type MongoGraphRepository struct {
	client     *mongo.Client
	db         *mongo.Database
	nodes      *mongo.Collection
	edges      *mongo.Collection
	projectID  string
}

// NewMongoGraphRepository connects to MongoDB and creates indexes.
// dsn is a standard MongoDB connection string (e.g., mongodb://localhost:27017/codergag).
func NewMongoGraphRepository(ctx context.Context, dsn, projectID string) (*MongoGraphRepository, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(dsn))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	dbName := "codergag"
	if dbName = extractDBName(dsn); dbName == "" {
		dbName = "codergag"
	}
	db := client.Database(dbName)
	nodes := db.Collection("graph_nodes")
	edges := db.Collection("graph_edges")
	r := &MongoGraphRepository{
		client:    client,
		db:        db,
		nodes:     nodes,
		edges:     edges,
		projectID: projectID,
	}
	if err := r.createIndexes(ctx); err != nil {
		return nil, fmt.Errorf("mongo indexes: %w", err)
	}
	return r, nil
}

func extractDBName(dsn string) string {
	parts := strings.Split(dsn, "/")
	if len(parts) > 0 {
		dbPart := strings.Split(parts[len(parts)-1], "?")[0]
		if dbPart != "" && dbPart != "codergag" {
			return dbPart
		}
	}
	return "codergag"
}

func (r *MongoGraphRepository) createIndexes(ctx context.Context) error {
	nodeIndexes := []string{"id", "kind", "project_id", "properties.identity"}
	for _, field := range nodeIndexes {
		_, err := r.nodes.Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys:    bson.D{{Key: field, Value: 1}},
			Options: options.Index().SetUnique(true),
		})
		if err != nil && !strings.Contains(err.Error(), "IndexOptionsConflict") {
			return err
		}
	}
	_, err := r.edges.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "from_id", Value: 1}}},
		{Keys: bson.D{{Key: "to_id", Value: 1}}},
		{Keys: bson.D{{Key: "kind", Value: 1}}},
	})
	return err
}

type mongoNode struct {
	ID         string         `bson:"_id" json:"id"`
	Kind       string         `bson:"kind" json:"kind"`
	ProjectID  string         `bson:"project_id" json:"project_id"`
	Properties bson.M         `bson:"properties" json:"properties"`
	CreatedAt  string         `bson:"created_at" json:"created_at"`
	UpdatedAt  string         `bson:"updated_at" json:"updated_at"`
}

type mongoEdge struct {
	ID         string         `bson:"_id" json:"id"`
	Kind       string         `bson:"kind" json:"kind"`
	FromID     string         `bson:"from_id" json:"from_id"`
	ToID       string         `bson:"to_id" json:"to_id"`
	Properties bson.M         `bson:"properties" json:"properties"`
	CreatedAt  string         `bson:"created_at" json:"created_at"`
}

func (r *MongoGraphRepository) UpsertNode(kind string, identity, properties map[string]any) (*models.Node, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	props := make(map[string]any, len(identity)+len(properties)+3)
	for k, v := range identity {
		props[k] = v
	}
	for k, v := range properties {
		props[k] = v
	}
	if _, ok := props["id"]; !ok {
		props["id"] = models.NewID()
	}
	if _, ok := props["created_at"]; !ok {
		props["created_at"] = now
	}
	if _, ok := props["updated_at"]; !ok {
		props["updated_at"] = now
	}
	if _, ok := props["project_id"]; !ok {
		props["project_id"] = identity["project_id"]
	}

	nodeID := props["id"].(string)
	projID, _ := props["project_id"].(string)

	mn := mongoNode{
		ID:         nodeID,
		Kind:       kind,
		ProjectID:  projID,
		Properties: toBSON(props),
		CreatedAt:  props["created_at"].(string),
		UpdatedAt:  props["updated_at"].(string),
	}

	_, err := r.nodes.InsertOne(ctx, mn)
	if err != nil {
		// Update if ID exists
		if strings.Contains(err.Error(), "E11000") {
			_, err = r.nodes.UpdateOne(ctx,
				bson.M{"_id": nodeID},
				bson.M{"$set": bson.M{
					"kind":       kind,
					"project_id": projID,
					"properties": bson.M{
						"$[elem]": bson.M{"$set": toBSON(props)},
					},
					"updated_at": now,
				}},
			)
			if err != nil {
				return nil, fmt.Errorf("update node: %w", err)
			}
		} else {
			return nil, fmt.Errorf("insert node: %w", err)
		}
	}

	node := &models.Node{
		ID:         nodeID,
		Kind:       kind,
		Properties: props,
	}
	return node, nil
}

func (r *MongoGraphRepository) GetNode(nodeID string) (*models.Node, error) {
	ctx := context.Background()
	var mn mongoNode
	err := r.nodes.FindOne(ctx, bson.M{"id": nodeID}).Decode(&mn)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &models.Node{
		ID:         mn.ID,
		Kind:       mn.Kind,
		Properties: fromBSON(mn.Properties),
	}, nil
}

func (r *MongoGraphRepository) FindNodes(kind string, filters map[string]any) ([]*models.Node, error) {
	ctx := context.Background()
	query := bson.M{}
	if kind != "" {
		query["kind"] = kind
	}
	for k, v := range filters {
		query["properties."+k] = v
	}

	cursor, err := r.nodes.Find(ctx, query)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []*models.Node
	for cursor.Next(ctx) {
		var mn mongoNode
		if err := cursor.Decode(&mn); err != nil {
			return nil, err
		}
		results = append(results, &models.Node{
			ID:         mn.ID,
			Kind:       mn.Kind,
			Properties: fromBSON(mn.Properties),
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results, nil
}

func (r *MongoGraphRepository) Link(kind, fromID, toID string, properties map[string]any) (*models.Edge, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	ctx := context.Background()
	if properties == nil {
		properties = make(map[string]any)
	}
	edge := models.NewEdge(kind, fromID, toID, properties)
	me := mongoEdge{
		ID:         edge.ID,
		Kind:       kind,
		FromID:     fromID,
		ToID:       toID,
		Properties: toBSON(properties),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	_, err := r.edges.InsertOne(ctx, me)
	if err != nil {
		return nil, fmt.Errorf("insert edge: %w", err)
	}
	return edge, nil
}

func (r *MongoGraphRepository) Neighbors(nodeID, edgeKind string, direction Direction) ([]EdgeNode, error) {
	ctx := context.Background()
	query := bson.M{}
	switch direction {
	case DirOut:
		query["from_id"] = nodeID
	case DirIn:
		query["to_id"] = nodeID
	case DirBoth:
		query["$or"] = []bson.M{{"from_id": nodeID}, {"to_id": nodeID}}
	}
	if edgeKind != "" {
		query["kind"] = edgeKind
	}

	cur, err := r.edges.Find(ctx, query)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	// Collect edge node IDs, then fetch neighbor nodes in batch
	type edgeResult struct {
		edge  *models.Edge
		neighborID string
	}
	var edgeResults []edgeResult
	for cur.Next(ctx) {
		var me mongoEdge
		if err := cur.Decode(&me); err != nil {
			return nil, err
		}
		e := &models.Edge{
			ID:         me.ID,
			Kind:       me.Kind,
			FromID:     me.FromID,
			ToID:       me.ToID,
			Properties: fromBSON(me.Properties),
		}
		var neighborID string
		switch direction {
		case DirOut:
			neighborID = me.ToID
		case DirIn:
			neighborID = me.FromID
		case DirBoth:
			if me.FromID == nodeID {
				neighborID = me.ToID
			} else {
				neighborID = me.FromID
			}
		}
		edgeResults = append(edgeResults, edgeResult{edge: e, neighborID: neighborID})
	}

	var results []EdgeNode
	for _, er := range edgeResults {
		var mn mongoNode
		if err := r.nodes.FindOne(ctx, bson.M{"id": er.neighborID}).Decode(&mn); err != nil {
			continue
		}
		n := &models.Node{
			ID:         mn.ID,
			Kind:       mn.Kind,
			Properties: fromBSON(mn.Properties),
		}
		results = append(results, EdgeNode{Edge: er.edge, Node: n})
	}
	return results, nil
}

func (r *MongoGraphRepository) RemoveNodes(nodeIDs []string) error {
	ctx := context.Background()
	if len(nodeIDs) == 0 {
		return nil
	}
	ids := make([]interface{}, len(nodeIDs))
	for i, id := range nodeIDs {
		ids[i] = id
	}
	_, err := r.nodes.DeleteMany(ctx, bson.M{"id": bson.M{"$in": ids}})
	if err != nil {
		return err
	}
	return nil
}

func (r *MongoGraphRepository) RemoveEdges(edgeIDs []string) error {
	ctx := context.Background()
	if len(edgeIDs) == 0 {
		return nil
	}
	ids := make([]interface{}, len(edgeIDs))
	for i, id := range edgeIDs {
		ids[i] = id
	}
	_, err := r.edges.DeleteMany(ctx, bson.M{"id": bson.M{"$in": ids}})
	if err != nil {
		return err
	}
	return nil
}

func (r *MongoGraphRepository) QueryReadonly(query string, params map[string]any) ([]map[string]any, error) {
	return nil, errors.New("advanced queries are not enabled by this repository")
}

func (r *MongoGraphRepository) Close() error {
	if r.client != nil {
		return r.client.Disconnect(context.Background())
	}
	return nil
}

// toBSON converts a map[string]any to bson.M, preserving nested structures.
func toBSON(m map[string]any) bson.M {
	result := make(bson.M, len(m))
	for k, v := range m {
		switch v := v.(type) {
		case map[string]any:
			result[k] = toBSON(v)
		default:
			result[k] = v
		}
	}
	return result
}

// fromBSON converts bson.M to map[string]any.
func fromBSON(m bson.M) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		if nested, ok := v.(bson.M); ok {
			result[k] = fromBSON(nested)
		} else {
			result[k] = v
		}
	}
	return result
}
