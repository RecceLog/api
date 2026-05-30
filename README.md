# RecceLog APIs overview

## Set up
Make sure to create a `.env` file in the root directory or export at least the environment variables `POSTGRES_USER`, `POSTGRES_PASSWORD` and `POSTGRES_DB` to create the database container and `DATABASE_CONNECTION_STRING` to connect the api to the database, `API_PORT` to specify the listening port of the api in the container and `CONTAINER_EXTERNAL_PORT` to specify the host listening port.
Other optional environment variables are
- `API_READ_TIMEOUT`: maximum time specified in seconds to read the request, if missing, default value is 5
- `API_WRITE_TIMEOUT`: maximum time specified in seconds to write the request, if missing, default value is 10
- `API_SHUTDOWN_TIMEOUT`: maximum time specified in seconds to shut down the server, if missing, default value is 15 seconds
- `DB_MAX_CONNECTIONS`: amount of maximum connection of the pool to the database, if missing, default value is 10
- `DB_MIN_CONNECTIONS`: amount of minimum connection of the pool to the database, if missing, default value is 0
- `DB_MAX_CONN_LIFETIME`: maximum time specified in hours that a connection of the pool can stay alive, if missing, default value is 1
- `DB_MAX_CONN_IDLE_TIME`: maximum time specified in minutes that a connection of the pool can stay in idle mode, if missing, default value is 30

## Local build
To run the api locally, you need to install `docker`, then, after following the `Set up` instruction, you can just run
```shell
docker compose up --build
```
then you can just make requests to `http://localhost:<CONTAINER_EXTERNAL_PORT>` from your host machine.

## Prod build
The `docker-compose.prod.yml` file is used to override some of the default configuration of `docker-compose.yml` and `docker-compose.override.yml` in order to have a container more suitable for a production host.
The command to create such a container is
```shell
docker compose -f docker-compose.yml -f docker-compose.prod.yml up --build -d
```

## Database migrations
Migrations are handled with `goose`. Check the `.env.example` file to see the configuration to make goose commands easier.
Once you added goose variables, you can run
- `goose up` to migrate the database to the most recent version available
- `goose up-by-one` to migrate the database up by 1
- `goose up-to <version>` to migrate the database to a specific <version>
- `goose down` to roll back the version by 1
- `goose down-to <version>` to roll back to a specific <version>
- `goose redo` to re-run the latest migration
- `goose reset` to roll back all migrations
- `goose status` to dump the migration status for the current database
- `goose version` to print the current version of the database
- `goose create <migration-name> [sql|go]` to create a new migration file with the current timestamp
- `goose fix` to apply sequential ordering to migrations
- `goose validate` to check migration files without running them
- 