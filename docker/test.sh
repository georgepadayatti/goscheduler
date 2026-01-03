#!/bin/bash

# Test script for GoScheduler job store examples
#
# Usage:
#   ./docker/test.sh [service]
#
# Services:
#   all        - Start all services (default)
#   postgres   - Start PostgreSQL only
#   mysql      - Start MySQL only
#   redis      - Start Redis only
#   mongodb    - Start MongoDB only
#   etcd       - Start etcd only
#   zookeeper  - Start Zookeeper only
#   stop       - Stop all services
#   clean      - Stop all services and remove volumes

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"

case "${1:-all}" in
    all)
        echo "Starting all services..."
        docker-compose -f "$COMPOSE_FILE" up -d
        echo ""
        echo "Services started. Connection details:"
        echo "  PostgreSQL: host=localhost port=5432 user=postgres password=postgres dbname=goscheduler"
        echo "  MySQL:      root:password@tcp(localhost:3306)/goscheduler"
        echo "  Redis:      localhost:6379"
        echo "  MongoDB:    mongodb://localhost:27017"
        echo "  etcd:       localhost:2379"
        echo "  Zookeeper:  localhost:2181"
        echo ""
        echo "To run examples:"
        echo "  go run ./examples/jobstores/postgresql"
        echo "  go run ./examples/jobstores/mysql"
        echo "  go run ./examples/jobstores/redis"
        echo "  go run ./examples/jobstores/mongodb"
        echo "  go run ./examples/jobstores/etcd"
        echo "  go run ./examples/jobstores/zookeeper"
        ;;
    postgres|postgresql)
        echo "Starting PostgreSQL..."
        docker-compose -f "$COMPOSE_FILE" up -d postgres
        echo "PostgreSQL started on port 5432"
        echo "Run: go run ./examples/jobstores/postgresql"
        ;;
    mysql)
        echo "Starting MySQL..."
        docker-compose -f "$COMPOSE_FILE" up -d mysql
        echo "MySQL started on port 3306"
        echo "Run: go run ./examples/jobstores/mysql"
        ;;
    redis)
        echo "Starting Redis..."
        docker-compose -f "$COMPOSE_FILE" up -d redis
        echo "Redis started on port 6379"
        echo "Run: go run ./examples/jobstores/redis"
        ;;
    mongodb|mongo)
        echo "Starting MongoDB..."
        docker-compose -f "$COMPOSE_FILE" up -d mongodb
        echo "MongoDB started on port 27017"
        echo "Run: go run ./examples/jobstores/mongodb"
        ;;
    etcd)
        echo "Starting etcd..."
        docker-compose -f "$COMPOSE_FILE" up -d etcd
        echo "etcd started on port 2379"
        echo "Run: go run ./examples/jobstores/etcd"
        ;;
    zookeeper|zk)
        echo "Starting Zookeeper..."
        docker-compose -f "$COMPOSE_FILE" up -d zookeeper
        echo "Zookeeper started on port 2181"
        echo "Run: go run ./examples/jobstores/zookeeper"
        ;;
    stop)
        echo "Stopping all services..."
        docker-compose -f "$COMPOSE_FILE" down
        echo "All services stopped."
        ;;
    clean)
        echo "Stopping all services and removing volumes..."
        docker-compose -f "$COMPOSE_FILE" down -v
        echo "All services stopped and volumes removed."
        ;;
    logs)
        docker-compose -f "$COMPOSE_FILE" logs -f
        ;;
    status)
        docker-compose -f "$COMPOSE_FILE" ps
        ;;
    *)
        echo "Unknown option: $1"
        echo "Usage: $0 [all|postgres|mysql|redis|mongodb|etcd|zookeeper|stop|clean|logs|status]"
        exit 1
        ;;
esac
