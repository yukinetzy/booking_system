package models

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var allowedHotelFields = map[string]struct{}{
	"title":           {},
	"description":     {},
	"location":        {},
	"address":         {},
	"price_per_night": {},
	"rating":          {},
	"ratingVotes":     {},
	"available_rooms": {},
	"amenities":       {},
	"imageUrl":        {},
}

func BuildHotelFilterFromQuery(query url.Values) bson.M {
	filter := bson.M{}

	city := strings.TrimSpace(query.Get("city"))
	if city != "" {
		filter["location"] = city
	}

	minPriceRaw := strings.TrimSpace(query.Get("minPrice"))
	maxPriceRaw := strings.TrimSpace(query.Get("maxPrice"))
	if minPriceRaw != "" || maxPriceRaw != "" {
		priceFilter := bson.M{}
		if minPrice, err := strconv.ParseFloat(minPriceRaw, 64); err == nil {
			priceFilter["$gte"] = minPrice
		}
		if maxPrice, err := strconv.ParseFloat(maxPriceRaw, 64); err == nil {
			priceFilter["$lte"] = maxPrice
		}
		if len(priceFilter) > 0 {
			filter["price_per_night"] = priceFilter
		}
	}

	if minRatingRaw := strings.TrimSpace(query.Get("minRating")); minRatingRaw != "" {
		if minRating, err := strconv.ParseFloat(minRatingRaw, 64); err == nil && minRating >= 1 && minRating <= 5 {
			filter["rating"] = bson.M{"$gte": minRating}
		}
	}

	q := strings.TrimSpace(query.Get("q"))
	if q != "" {
		safePattern := regexp.QuoteMeta(q)
		filter["$or"] = bson.A{
			bson.M{"title": bson.M{"$regex": safePattern, "$options": "i"}},
			bson.M{"description": bson.M{"$regex": safePattern, "$options": "i"}},
			bson.M{"location": bson.M{"$regex": safePattern, "$options": "i"}},
			bson.M{"address": bson.M{"$regex": safePattern, "$options": "i"}},
			bson.M{"amenities": bson.M{"$regex": safePattern, "$options": "i"}},
		}
	}

	return filter
}

func BuildHotelSortFromQuery(sortKey string) bson.D {
	switch strings.TrimSpace(sortKey) {
	case "price_asc":
		return bson.D{{Key: "price_per_night", Value: 1}, {Key: "title", Value: 1}}
	case "price_desc":
		return bson.D{{Key: "price_per_night", Value: -1}, {Key: "title", Value: 1}}
	case "rating_desc":
		return bson.D{{Key: "rating", Value: -1}, {Key: "price_per_night", Value: 1}}
	case "title_asc":
		return bson.D{{Key: "title", Value: 1}, {Key: "price_per_night", Value: 1}}
	case "title_desc":
		return bson.D{{Key: "title", Value: -1}, {Key: "price_per_night", Value: 1}}
	default:
		return bson.D{{Key: "rating", Value: -1}, {Key: "price_per_night", Value: 1}}
	}
}

func BuildHotelProjectionFromQuery(fields string) bson.M {
	fields = strings.TrimSpace(fields)
	if fields == "" {
		return nil
	}

	projection := bson.M{}
	for _, field := range strings.Split(fields, ",") {
		name := strings.TrimSpace(field)
		if _, ok := allowedHotelFields[name]; ok {
			projection[name] = 1
		}
	}

	if len(projection) == 0 {
		return nil
	}
	return projection
}

func (s *Store) FindHotels(ctx context.Context, filter bson.M, sortQuery bson.D, projection bson.M, skip int64, limit int64) ([]bson.M, int64, error) {
	findOptions := options.Find().SetSort(sortQuery).SetSkip(skip).SetLimit(limit)
	if projection != nil {
		findOptions.SetProjection(projection)
	}

	cursor, err := s.collection("hotels").Find(ctx, filter, findOptions)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	items := make([]bson.M, 0)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, 0, err
	}

	total, err := s.collection("hotels").CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func (s *Store) FindHotelByID(ctx context.Context, id string, projection bson.M) (bson.M, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, nil
	}

	findOptions := options.FindOne()
	if projection != nil {
		findOptions.SetProjection(projection)
	}

	var hotel bson.M
	err = s.collection("hotels").FindOne(ctx, bson.M{"_id": objectID}, findOptions).Decode(&hotel)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return hotel, nil
}

func (s *Store) CreateHotel(ctx context.Context, hotel bson.M, userID string) (string, error) {
	now := time.Now().UTC()
	hotelDoc := bson.M{}
	for key, value := range hotel {
		hotelDoc[key] = value
	}

	if ratingVotes, ok := toInt(hotelDoc["ratingVotes"]); ok {
		hotelDoc["ratingVotes"] = ratingVotes
	} else {
		hotelDoc["ratingVotes"] = 0
	}

	if ratingTotal, ok := toFloat(hotelDoc["ratingTotal"]); ok {
		hotelDoc["ratingTotal"] = ratingTotal
	} else {
		hotelDoc["ratingTotal"] = 0.0
	}

	if creatorID, err := primitive.ObjectIDFromHex(userID); err == nil {
		hotelDoc["createdBy"] = creatorID
	} else {
		hotelDoc["createdBy"] = nil
	}

	hotelDoc["createdAt"] = now
	hotelDoc["updatedAt"] = now

	result, err := s.collection("hotels").InsertOne(ctx, hotelDoc)
	if err != nil {
		return "", err
	}

	insertedID, ok := result.InsertedID.(primitive.ObjectID)
	if !ok {
		return "", errors.New("invalid inserted id type")
	}

	return insertedID.Hex(), nil
}

func (s *Store) UpdateHotelByID(ctx context.Context, id string, hotel bson.M) (int64, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return 0, nil
	}

	updateFields := bson.M{}
	for key, value := range hotel {
		updateFields[key] = value
	}
	updateFields["updatedAt"] = time.Now().UTC()

	result, err := s.collection("hotels").UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{"$set": updateFields})
	if err != nil {
		return 0, err
	}

	return result.MatchedCount, nil
}

func (s *Store) DeleteHotelByID(ctx context.Context, id string) (int64, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return 0, nil
	}

	result, err := s.collection("hotels").DeleteOne(ctx, bson.M{"_id": objectID})
	if err != nil {
		return 0, err
	}

	return result.DeletedCount, nil
}

func (s *Store) DistinctHotelCities(ctx context.Context) ([]string, error) {
	values, err := s.collection("hotels").Distinct(ctx, "location", bson.M{})
	if err != nil {
		return nil, err
	}

	cities := make([]string, 0, len(values))
	for _, value := range values {
		city := strings.TrimSpace(asString(value))
		if city != "" {
			cities = append(cities, city)
		}
	}

	sort.Strings(cities)
	return cities, nil
}

func (s *Store) RateHotelByID(ctx context.Context, id string, score int) (int64, float64, int, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return 0, 0, 0, nil
	}
	if score < 1 || score > 5 {
		return 0, 0, 0, nil
	}

	var hotel bson.M
	err = s.collection("hotels").FindOne(
		ctx,
		bson.M{"_id": objectID},
		options.FindOne().SetProjection(bson.M{"rating": 1, "ratingTotal": 1, "ratingVotes": 1}),
	).Decode(&hotel)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, 0, 0, nil
	}
	if err != nil {
		return 0, 0, 0, err
	}

	fallbackRating, _ := toFloat(hotel["rating"])
	currentVotes, hasVotes := toInt(hotel["ratingVotes"])
	if !hasVotes || currentVotes <= 0 {
		currentVotes = 1
	}

	currentTotal, hasTotal := toFloat(hotel["ratingTotal"])
	if !hasTotal || currentTotal <= 0 {
		currentTotal = fallbackRating
	}

	nextVotes := currentVotes + 1
	nextTotal := currentTotal + float64(score)
	nextRating := math.Round((nextTotal/float64(nextVotes))*10) / 10

	result, err := s.collection("hotels").UpdateOne(
		ctx,
		bson.M{"_id": objectID},
		bson.M{"$set": bson.M{
			"rating":      nextRating,
			"ratingVotes": nextVotes,
			"ratingTotal": nextTotal,
			"updatedAt":   time.Now().UTC(),
		}},
	)
	if err != nil {
		return 0, 0, 0, err
	}

	return result.MatchedCount, nextRating, nextVotes, nil
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return number, true
	default:
		return 0, false
	}
}

func toInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		if math.Mod(typed, 1) != 0 {
			return 0, false
		}
		return int(typed), true
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return number, true
	default:
		return 0, false
	}
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
