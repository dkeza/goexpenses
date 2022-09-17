# goexpenses
Simple expenses web application written in Go, using Echo and PostgresSQL.
You must manualy execute script pg_structure.sql on Postgres database first.
You can define expenses and incomes, and then enter posts.
It is possible to enter amounts in RSD or EUR currency.

Project is using modules for dependency management.

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

Alternative to ini file is use of enviroment variables.
Look in startgoexpenses.dev.bat for use in Windows.

Start binary

Database would be automatically created. EUR and RSD currency exchange rates would be automatically updated on start, and then once a day.
User must register with valid E-Mail. When reseting password, activation link is sent to E-Mail.

Working example:
https://goexpenses.kezic.net/

Credits to

* [Echo Web Framework](https://github.com/labstack/echo)
* [open exchange rates](https://openexchangerates.org)
