# goexpenses
Simple expenses web application written in Go, using Echo and PostgresSQL.
You must manualy execute script pg_structure.sql on Postgres database first.
You can define expenses and incomes, and then enter posts.
It is possible to enter amounts in RSD or EUR currency.

Project is using modules for dependency management.

Requires Go 1.27.1 or newer.

Build and installation

Get source with

```
go get github.com/dkeza/goexpenses
```

Build binary

```
go build
```

Copy the executable to the server. HTML templates, static files and database
structure scripts are embedded in it and do not need to be copied separately.

Runtime configuration remains external. You can copy the example configuration
file

```
goexpenses.dev.ini
```

Rename goexpenses.dev.ini to goexpenses.ini, and enter settings.

Alternatively, use environment variables without an INI file. Environment
variables override values loaded from `goexpenses.ini`. Look in
`startgoexpenses.dev.bat` for a Windows example.

If SQLite is used, its database file must remain outside the executable so that
application data persists between deployments.

Session cookies are secure by default. Set `COOKIE_SECURE=false` (or
`cookiesecure=false` in `goexpenses.ini`) only for local development over HTTP.

Start binary exe

Database would be automatically created. EUR and RSD currency exchange rates would be automatically updated on start, and then once a day.
User must register with valid E-Mail. When reseting password, activation link is sent to E-Mail.

Working example:
https://goexpenses.kezic.net/

Credits to

* [Echo Web Framework](https://github.com/labstack/echo)
* [open exchange rates](https://openexchangerates.org)
