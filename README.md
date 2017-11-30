# goexpenses
Simple expenses web application written in Go, using Echo and SQLite

Build and installation

Get source with

```
go get github.com/dkeza/goexpenses
```

Build binary

```
go build
```

Copy folders and files to install folder

```
db
static
templates
goexpenses.dev.ini
```

Rename goexpenses.dev.ini to goexpenses.ini, and enter settings.

Start binary

Database would be automatically created. EUR and RSD currency exchange rates would be automatically updated on start, and then once a day.
User must register with valid E-Mail. When reseting password, activation link is sent to E-Mail.

Credits to

* [Echo Web Framework](https://github.com/labstack/echo)