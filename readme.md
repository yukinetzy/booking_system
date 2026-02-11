# EasyBooking Final Project (Go Backend)

Production-ready web application with backend fully migrated from Node.js to Go.

## Tech Stack
- Go + Chi Router
- MongoDB Go Driver
- Cookie + Mongo-backed sessions
- bcrypt password hashing

## Architecture
- `cmd/server` - HTTP server entrypoint
- `cmd/role` - role management CLI (`grant/revoke/show/list`)
- `internal/config` - env loading and validation
- `internal/db` - Mongo connection and startup maintenance
- `internal/session` - session manager and persistence
- `internal/middleware` - logging, recovery, auth, static middleware
- `internal/models` - Mongo data access layer
- `internal/handlers` - web + API handlers
- `internal/utils` - validation and pagination
- `internal/view` - HTML renderer with `{{placeholder}}` replacement

## Final Project Requirements Coverage
- Modular backend structure in Go packages
- Related collections:
  - `users`
  - `hotels`
  - `bookings` (references `userId` + `roomId`)
  - `room_calendar` (atomic no-double-booking slots)
  - `waitlist` (subscriptions for busy date ranges)
  - `notifications` (in-app notifications)
  - `contact_requests`
  - `sessions`
- Authentication:
  - login / logout / register
  - session-based auth (cookie + Mongo)
  - bcrypt
- Authorization + roles:
  - roles: `user`, `admin`
  - admin can manage hotels and all bookings
  - user can manage only own bookings
- API security:
  - write endpoints protected
  - no public update/delete endpoints
  - validation + safe error handling
- Pagination:
  - hotels and bookings list endpoints support pagination metadata
- Booking consistency:
  - atomic anti-overbooking protection for overlapping dates
  - waitlist subscription + release-driven notifications
- Environment-based secrets:
  - no hardcoded secrets required for startup

## Environment Variables (`.env`)
```env
PORT=3000
MONGO_URI=your_mongo_connection_string
DB_NAME=easybook_final
DNS_SERVERS=8.8.8.8,1.1.1.1
SESSION_SECRET=your_long_random_secret
```

## Run
```bash
go mod tidy
go run ./cmd/server
```

## Role Management
```bash
go run ./cmd/role list
go run ./cmd/role show <email>
go run ./cmd/role grant <email>
go run ./cmd/role revoke <email>
```

## Main Web Routes
- `GET /hotels` (public)
- `GET /hotels/:id` (public)
- `GET /bookings` (auth required)
- `GET /login`, `POST /login`
- `GET /register`, `POST /register`
- `GET /contact`, `POST /contact`
- `GET /notifications` (auth required)
- `POST /logout`

## Main API Routes
- `GET /api/auth/session`
- `GET /api/hotels`
- `GET /api/hotels/:id`
- `POST /api/hotels` (admin)
- `PUT /api/hotels/:id` (admin)
- `DELETE /api/hotels/:id` (admin)
- `POST /api/hotels/:id/rate` (auth)
- `GET /api/bookings` (auth)
- `GET /api/bookings/availability` (auth)
- `GET /api/bookings/:id` (owner or admin)
- `POST /api/bookings` (auth)
- `PUT /api/bookings/:id` (owner or admin)
- `DELETE /api/bookings/:id` (owner or admin)
- `POST /api/notifications/subscribe` (auth)
- `GET /api/notifications` (auth)
- `POST /api/notifications/:id/read` (auth)
- `POST /api/notifications/read-all` (auth)
