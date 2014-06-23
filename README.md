# jobrunner

This is a work in progress. The intent is to provide a HTTP+JSON API that allows
callers to run jobs that on the box. Once a job is running, the callers can get
a chunked HTTP response of the stdout/stderr stream.

A job consists of one or more commands. Right now a job can only be a single
command.

Future updates:
* Allow callers to set the working directory for a command.
* Allow jobs to be a sequence of commands.
* Allow a command to be a Docker container.
* Allow jobs to be a mix of uncontained processes and Docker containers.

**WARNING** This service is currently completely unsecured and should not be run
anywhere near a production environment.

## Building

A Makefile is provided that adds the vendor directory to the GOPATH. To build
jobrunner:

    make

To run the unit tests:

    make test

To clean stuff up:

    make clean

You can also do the usual "go build ." and "go test", just make sure to add the
vendor directory to your GOPATH.

## Configuring

All configuration is handled through environment variables.

Set **JOBRUNNER_PORT** to the port you want jobrunner to listen on. The default is
8080.

Set **JOBRUNNER_HOSTNAME** to the hostname you want jobrunner to use. The default is
"".

## Running

Place the jobrunner executable on the path and set the environment variables
listed above (if necessary). Then simply run 'jobrunner'.

## HTTP+JSON API

**NOTE:** All of this is subject to change.

### GET /
Lists the IDs for the jobs that are currently running. The response will look
like this:

```json
{
    "IDs": [
        "4663c092-7e61-46ea-9476-4dda5eea66d6",
        "f5202acc-dba6-4bc7-a8ba-0fdf201ac0a5",
        "c86e4b0c-1f11-4c7b-b0bf-e50b0886e06d"
    ]
}
```

### POST /
Submits a job. The request JSON should look like this:

```json
{
    "CommandLine" : "<bash command to run>",
    "Environment" : {
        "ENV_VAR" : "setting"
    }
}
```

The return value will contain the UUID assigned to the running job. The UUID is
only unique to a particular instance of jobrunner. Here's what the response
containing the UUID looks like:

```json
{
    "ID":"09eb8270-0c98-4213-854e-042dc0745dd3"
}
```

### GET /:ID/attach
Attaches to the running job and returns a chunked response of the output from
stdout/stderr.

### DELETE /:ID
Kills a running job. Only returns a 200 status, even if the job doesn't exist
in job runner.
