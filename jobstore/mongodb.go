package jobstore

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/georgepadayatti/goscheduler/job"
	jobtrigger "github.com/georgepadayatti/goscheduler/trigger"
)

// MongoDBJobStore stores jobs in a MongoDB database.
type MongoDBJobStore struct {
	client     *mongo.Client
	collection *mongo.Collection
	scheduler  any
	alias      string
	ctx        context.Context
	ownsClient bool
}

// MongoDBJobStoreConfig configures the MongoDB job store.
type MongoDBJobStoreConfig struct {
	// Existing MongoDB client (takes precedence)
	Client *mongo.Client

	// Connection URI (used if Client is nil)
	// e.g., "mongodb://localhost:27017"
	URI string

	// Database and collection names
	Database   string // default: "goscheduler"
	Collection string // default: "jobs"
}

// NewMongoDBJobStore creates a new MongoDB job store.
func NewMongoDBJobStore(cfg MongoDBJobStoreConfig) (*MongoDBJobStore, error) {
	ctx := context.Background()
	var client *mongo.Client
	var err error
	var ownsClient bool

	if cfg.Client != nil {
		client = cfg.Client
		ownsClient = false
	} else if cfg.URI != "" {
		clientOpts := options.Client().ApplyURI(cfg.URI)
		client, err = mongo.Connect(ctx, clientOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
		}
		ownsClient = true
	} else {
		return nil, fmt.Errorf("either Client or URI must be provided")
	}

	database := cfg.Database
	if database == "" {
		database = "goscheduler"
	}

	collectionName := cfg.Collection
	if collectionName == "" {
		collectionName = "jobs"
	}

	collection := client.Database(database).Collection(collectionName)

	return &MongoDBJobStore{
		client:     client,
		collection: collection,
		ctx:        ctx,
		ownsClient: ownsClient,
	}, nil
}

// Start initializes the job store and creates indexes.
func (s *MongoDBJobStore) Start(scheduler any, alias string) error {
	s.scheduler = scheduler
	s.alias = alias

	// Ping to verify connection
	if err := s.client.Ping(s.ctx, nil); err != nil {
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	// Create index on next_run_time
	indexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "next_run_time", Value: 1}},
		Options: options.Index().
			SetSparse(true).
			SetName("idx_next_run_time"),
	}

	_, err := s.collection.Indexes().CreateOne(s.ctx, indexModel)
	if err != nil {
		// Index might already exist, ignore
	}

	return nil
}

// Shutdown closes the MongoDB connection if we own it.
func (s *MongoDBJobStore) Shutdown() error {
	if s.ownsClient && s.client != nil {
		return s.client.Disconnect(s.ctx)
	}
	return nil
}

// LookupJob returns a job by ID.
func (s *MongoDBJobStore) LookupJob(jobID string) (*job.Job, error) {
	var doc struct {
		JobState []byte `bson:"job_state"`
	}

	err := s.collection.FindOne(s.ctx, bson.M{"_id": jobID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lookup job: %w", err)
	}

	return s.deserializeJob(doc.JobState)
}

// GetDueJobs returns jobs with next_run_time <= now.
func (s *MongoDBJobStore) GetDueJobs(now time.Time) ([]*job.Job, error) {
	timestamp := float64(now.UTC().Unix()) + float64(now.Nanosecond())/1e9

	filter := bson.M{
		"next_run_time": bson.M{"$lte": timestamp},
	}

	opts := options.Find().SetSort(bson.D{{Key: "next_run_time", Value: 1}})

	cursor, err := s.collection.Find(s.ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get due jobs: %w", err)
	}
	defer cursor.Close(s.ctx)

	return s.decodeJobsFromCursor(cursor)
}

// GetNextRunTime returns the earliest next run time.
func (s *MongoDBJobStore) GetNextRunTime() *time.Time {
	filter := bson.M{
		"next_run_time": bson.M{"$ne": nil},
	}

	opts := options.FindOne().
		SetSort(bson.D{{Key: "next_run_time", Value: 1}}).
		SetProjection(bson.M{"next_run_time": 1})

	var doc struct {
		NextRunTime float64 `bson:"next_run_time"`
	}

	err := s.collection.FindOne(s.ctx, filter, opts).Decode(&doc)
	if err != nil {
		return nil
	}

	sec := int64(doc.NextRunTime)
	nsec := int64((doc.NextRunTime - float64(sec)) * 1e9)
	t := time.Unix(sec, nsec).UTC()
	return &t
}

// GetAllJobs returns all jobs sorted by next run time.
func (s *MongoDBJobStore) GetAllJobs() ([]*job.Job, error) {
	// Sort by next_run_time ascending, with null values last
	opts := options.Find().SetSort(bson.D{
		{Key: "next_run_time", Value: 1},
		{Key: "_id", Value: 1},
	})

	cursor, err := s.collection.Find(s.ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get all jobs: %w", err)
	}
	defer cursor.Close(s.ctx)

	jobs, err := s.decodeJobsFromCursor(cursor)
	if err != nil {
		return nil, err
	}

	// Fix paused jobs sorting (MongoDB doesn't sort nulls last by default)
	s.fixPausedJobsSorting(jobs)

	return jobs, nil
}

// AddJob adds a job to the store.
func (s *MongoDBJobStore) AddJob(j *job.Job) error {
	if j.FuncRef == "" {
		return &TransientJobError{JobID: j.ID}
	}

	stateData, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	var timestamp *float64
	if j.NextRunTime != nil {
		ts := float64(j.NextRunTime.UTC().Unix()) + float64(j.NextRunTime.Nanosecond())/1e9
		timestamp = &ts
	}

	doc := bson.M{
		"_id":           j.ID,
		"next_run_time": timestamp,
		"job_state":     stateData,
	}

	_, err = s.collection.InsertOne(s.ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return &ConflictingIDError{JobID: j.ID}
		}
		return fmt.Errorf("failed to add job: %w", err)
	}

	return nil
}

// UpdateJob updates an existing job.
func (s *MongoDBJobStore) UpdateJob(j *job.Job) error {
	stateData, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	var timestamp *float64
	if j.NextRunTime != nil {
		ts := float64(j.NextRunTime.UTC().Unix()) + float64(j.NextRunTime.Nanosecond())/1e9
		timestamp = &ts
	}

	update := bson.M{
		"$set": bson.M{
			"next_run_time": timestamp,
			"job_state":     stateData,
		},
	}

	result, err := s.collection.UpdateOne(s.ctx, bson.M{"_id": j.ID}, update)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	if result.MatchedCount == 0 {
		return &JobLookupError{JobID: j.ID}
	}

	return nil
}

// RemoveJob removes a job by ID.
func (s *MongoDBJobStore) RemoveJob(jobID string) error {
	result, err := s.collection.DeleteOne(s.ctx, bson.M{"_id": jobID})
	if err != nil {
		return fmt.Errorf("failed to remove job: %w", err)
	}

	if result.DeletedCount == 0 {
		return &JobLookupError{JobID: jobID}
	}

	return nil
}

// RemoveAllJobs removes all jobs.
func (s *MongoDBJobStore) RemoveAllJobs() error {
	_, err := s.collection.DeleteMany(s.ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("failed to remove all jobs: %w", err)
	}
	return nil
}

// serializeJob serializes a job to bytes.
func (s *MongoDBJobStore) serializeJob(j *job.Job) ([]byte, error) {
	state, err := j.GetState()
	if err != nil {
		return nil, fmt.Errorf("failed to get job state: %w", err)
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(state); err != nil {
		return nil, fmt.Errorf("failed to encode job: %w", err)
	}

	return buf.Bytes(), nil
}

// deserializeJob deserializes a job from bytes.
func (s *MongoDBJobStore) deserializeJob(data []byte) (*job.Job, error) {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	var state job.JobState
	if err := dec.Decode(&state); err != nil {
		return nil, fmt.Errorf("failed to decode job: %w", err)
	}

	j := &job.Job{
		ID:               state.ID,
		Name:             state.Name,
		FuncRef:          state.FuncRef,
		Args:             state.Args,
		Executor:         state.Executor,
		JobStore:         state.JobStore,
		Coalesce:         state.Coalesce,
		MisfireGraceTime: state.MisfireGraceTime,
		MaxInstances:     state.MaxInstances,
		NextRunTime:      state.NextRunTime,
	}

	if j.JobStore == "" && s.alias != "" {
		j.JobStore = s.alias
	}

	// Restore the function from the registry using FuncRef
	if state.FuncRef != "" {
		if fn, ok := job.LookupFunc(state.FuncRef); ok {
			j.Func = fn
		}
	}

	// Restore the trigger from serialized data
	if len(state.TriggerData) > 0 {
		trigBuf := bytes.NewBuffer(state.TriggerData)
		trigDec := gob.NewDecoder(trigBuf)
		var trig jobtrigger.Trigger
		if err := trigDec.Decode(&trig); err == nil && !isNilValue(trig) {
			j.Trigger = trig
		} else {
			j.SetRawTriggerData(state.TriggerData, state.TriggerType)
		}
	}

	return j, nil
}

// decodeJobsFromCursor decodes jobs from a MongoDB cursor.
func (s *MongoDBJobStore) decodeJobsFromCursor(cursor *mongo.Cursor) ([]*job.Job, error) {
	var jobs []*job.Job
	var failedIDs []string

	for cursor.Next(s.ctx) {
		var doc struct {
			ID       string `bson:"_id"`
			JobState []byte `bson:"job_state"`
		}

		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		j, err := s.deserializeJob(doc.JobState)
		if err != nil {
			failedIDs = append(failedIDs, doc.ID)
			continue
		}

		jobs = append(jobs, j)
	}

	// Remove corrupted jobs
	if len(failedIDs) > 0 {
		s.collection.DeleteMany(s.ctx, bson.M{"_id": bson.M{"$in": failedIDs}})
	}

	return jobs, cursor.Err()
}

// fixPausedJobsSorting moves paused jobs (nil next_run_time) to the end.
func (s *MongoDBJobStore) fixPausedJobsSorting(jobs []*job.Job) {
	// Find the first non-paused job
	for i, j := range jobs {
		if j.NextRunTime != nil {
			if i > 0 {
				// Move paused jobs to the end
				pausedJobs := jobs[:i]
				copy(jobs, jobs[i:])
				copy(jobs[len(jobs)-i:], pausedJobs)
			}
			break
		}
	}
}
