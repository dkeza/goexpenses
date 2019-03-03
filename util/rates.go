package util

import (
	"log"
	"math"
	"net/http"

	"github.com/dkeza/goexpenses/database"

	"github.com/buger/jsonparser"

	"fmt"
	"io/ioutil"
	"strconv"
	"time"
)

func GetExchangeRates() (float64, string) {
	var kurs float64
	date := time.Now().Format("2006-01-02 15:04:05")
	log.Println("Getting exchange rates from internet")
	r, err := http.Get("https://openexchangerates.org/api/latest.json?app_id=" + Settings.OpenExchangeRatesId)
	if err == nil {

		defer r.Body.Close()
		body, _ := ioutil.ReadAll(r.Body)

		value, dataType, offset, err := jsonparser.Get(body, "rates", "EUR")
		x := string(value)
		value, dataType, offset, err = jsonparser.Get(body, "rates", "RSD")
		y := string(value)
		eur, _ := strconv.ParseFloat(x, 32)
		rsd, _ := strconv.ParseFloat(y, 32)
		kurs = ToFixed(rsd/eur, 4)
		log.Println(kurs, dataType, offset, err)
		log.Println("Kurs:", kurs)
		if kurs > 0.00 {

			count := 0
			database.Db.Get(&count, "SELECT COUNT(*) FROM currencies WHERE code = 'EUR'")
			if count == 0 {
				log.Println("Insert EUR record")
				sql := fmt.Sprintf(`INSERT INTO currencies (code) VALUES (%v)`, SqlParam(1))
				database.Db.MustExec(sql, `EUR`)
			}

			sql := fmt.Sprintf(`UPDATE currencies SET rate = %v, date = %v WHERE code = %v`, SqlParam(1), SqlParam(2), SqlParam(3))
			err1 := database.Db.MustExec(sql, kurs, date, `EUR`)
			log.Println(err1)
		}

	}
	return kurs, date
}

func ToFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(Round(num*output)) / output
}

func Round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}
