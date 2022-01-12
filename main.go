package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rdoorn/gohelper/statsdhelper"
)

type Handler struct {
	statsd     *statsdhelper.Handler
	easyEnergy *EasyEnergy
	last       time.Time
}

type EasyEnergy struct {
	Tarief EasyEnergyTarief
	m      sync.Mutex
}

type EasyEnergyTarief struct {
	// leverancier
	SpotPrijsStroomKwh             float64 `json:spotprijsstroomkwh`
	SpotPrijsStroomTerugKwh        float64 `json:spotprijsstroomterugkwh`
	OpslagEasyEnergyPrijsStroomKwh float64 `json:opslageasyenergyprijsstroomkwh`
	VergroeningPrijsStroomKwh      float64 `json:vergroeningprijsstroomkwh`

	// overheid
	EnergieBelastingStroomKwh      float64 `json:energiebelasgingstroomkwh`
	OpslagDuurzameEnergieStroomKwh float64 `json:opslagduurzameenergiestroomkwh`
	BtwStroom                      float64 `json:btwstroom`

	// levernacier
	SpotPrijsGasM3             float64 `json:spotprijsgasm3`
	SpotPrijsGasTerugM3        float64 `json:spotprijsgasterugm3`
	OpslagEasyEnergyPrijsGasM3 float64 `json:opslageasyenergyprijsgaskwh`

	// overheid
	OpslagRegioPrijsGasM3      float64 `json:opslagregioprijsgaskwh`
	EnergieBelastingGasM3      float64 `json:energiebelasgingsgaskwh`
	OpslagDuurzameEnergieGasM3 float64 `json:opslagduurzameenergiesgaskwh`
	BtwGas                     float64 `json:btwgas`

	TotalPrijsStroomKwh float64 `json:totalprijsstroomkwh`
	TotalPrijsGasM3     float64 `json:totalprijsgasm3`
}

/*type EasyEnergySpot struct {
	Items []EasyEnergySpotItem
}*/

type EasyEnergySpotItem struct {
	TimeStamp    time.Time `json:Timestamp`
	SupplierId   int       `json:SupplierId`
	TariffUsage  float64   `json:TariffUsage`
	TariffReturn float64   `json:TariffReturn`
}

func getJson(url string, target interface{}) error {
	myClient := &http.Client{Timeout: 10 * time.Second}

	r, err := myClient.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(target)
}

func (e *EasyEnergyTarief) GetSpot(s string) (error, float64, float64) {
	items := []EasyEnergySpotItem{}

	err := getJson(fmt.Sprintf("https://mijn.easyenergy.com/nl/api/tariff/get%stariffs?startTimestamp=2022-01-11T23:00:00.000Z&endTimestamp=2022-01-12T23:00:00.000Z&grouping=", s), &items)
	if err != nil {
		return err, 0, 0
	}

	for _, i := range items {
		if i.TimeStamp.Before(time.Now()) && i.TimeStamp.Add(1*time.Hour).After(time.Now()) {
			return nil, i.TariffUsage, i.TariffReturn
		}
	}

	log.Printf("spot data returned: %+v", items)
	return fmt.Errorf("no match found for current date"), 0, 0
}

// GetMisc get misc values
func (e *EasyEnergyTarief) GetMisc() error {

	myClient := &http.Client{Timeout: 10 * time.Second}

	r, err := myClient.Get("https://www.easyenergy.com/nl/energietarieven")
	if err != nil {
		return err
	}
	defer r.Body.Close()

	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}

	log.Printf("got data: %s", data)

	// opslag (0,968 ct/kWh incl. BTW voor stroom en 4,7 ct/m3 incl. BTW voor gas),
	// regiotoeslag (enkel bij gas, 2,197 ct/m3 incl. BTW),
	// Energiebelasting (4,452 ct/kWh incl. BTW voor stroom en 43,950 ct/m3 incl. BTW voor gas)
	// Opslag Duurzame Energie (3,691 ct/kWh incl. BTW voor stroom en 10,467 ct/m3 incl. BTW voor gas)
	// kosten voor de vergoening van de stroom (GvO, 0,064 ct/kWh incl. BTW).</p>

	// Opslag
	re := regexp.MustCompile(`opslag \((\d+,\d+) ct/kWh incl. BTW voor stroom en (\d+,\d+) ct/m3`)
	match := re.FindStringSubmatch(string(data))
	if len(match) == 3 {
		if s, err := strconv.ParseFloat(strings.Replace(match[1], ",", ".", -1), 64); err == nil {
			e.OpslagEasyEnergyPrijsStroomKwh = s / 100
		}
		if s, err := strconv.ParseFloat(strings.Replace(match[2], ",", ".", -1), 64); err == nil {
			e.OpslagEasyEnergyPrijsGasM3 = s / 100
		}
	}

	// Regiotoeslag
	re = regexp.MustCompile(`regiotoeslag \(enkel bij gas, (\d+,\d+) ct/m3`)
	match = re.FindStringSubmatch(string(data))
	if len(match) == 2 {
		if s, err := strconv.ParseFloat(strings.Replace(match[1], ",", ".", -1), 64); err == nil {
			e.OpslagRegioPrijsGasM3 = s / 100
		}
	}

	// Energie belazting (EB)
	re = regexp.MustCompile(`Energiebelasting \((\d+,\d+) ct/kWh incl. BTW voor stroom en (\d+,\d+) ct/m3`)
	match = re.FindStringSubmatch(string(data))
	if len(match) == 3 {
		if s, err := strconv.ParseFloat(strings.Replace(match[1], ",", ".", -1), 64); err == nil {
			e.EnergieBelastingStroomKwh = s / 100
		}
		if s, err := strconv.ParseFloat(strings.Replace(match[2], ",", ".", -1), 64); err == nil {
			e.EnergieBelastingGasM3 = s / 100
		}
	}

	// Opslag duurzame Energie (ODE)
	re = regexp.MustCompile(`Opslag Duurzame Energie \((\d+,\d+) ct/kWh incl. BTW voor stroom en (\d+,\d+) ct/m3`)
	match = re.FindStringSubmatch(string(data))
	if len(match) == 3 {
		if s, err := strconv.ParseFloat(strings.Replace(match[1], ",", ".", -1), 64); err == nil {
			e.OpslagDuurzameEnergieStroomKwh = s / 100
		}
		if s, err := strconv.ParseFloat(strings.Replace(match[2], ",", ".", -1), 64); err == nil {
			e.OpslagDuurzameEnergieGasM3 = s / 100
		}
	}

	// Vergroening Stroom
	re = regexp.MustCompile(`kosten voor de vergoening van de stroom \(GvO, (\d+,\d+) ct/kWh`)
	match = re.FindStringSubmatch(string(data))
	if len(match) == 2 {
		if s, err := strconv.ParseFloat(strings.Replace(match[1], ",", ".", -1), 64); err == nil {
			e.VergroeningPrijsStroomKwh = s / 100
		}
	}

	e.BtwGas = float64(21)
	e.BtwStroom = float64(21)
	//	log.Printf("match: %+v", match)

	// log.Printf("result: %+v", e)

	return fmt.Errorf("unable to parse html")
}

func (e *EasyEnergyTarief) GetData() {

	e.GetMisc()

	if err, tarif, terug := e.GetSpot("leba"); err == nil {
		e.SpotPrijsGasM3 = tarif
		e.SpotPrijsGasTerugM3 = terug
	}

	if err, tarif, terug := e.GetSpot("apx"); err == nil {
		e.SpotPrijsStroomKwh = tarif
		e.SpotPrijsStroomTerugKwh = terug
	}

	e.TotalPrijsGasM3 = (e.SpotPrijsGasM3 + e.OpslagEasyEnergyPrijsGasM3 + e.OpslagDuurzameEnergieGasM3 + e.EnergieBelastingGasM3 + e.OpslagRegioPrijsGasM3) * (1 + (e.BtwGas / 100))
	e.TotalPrijsStroomKwh = (e.SpotPrijsStroomKwh + e.OpslagEasyEnergyPrijsStroomKwh + e.OpslagDuurzameEnergieStroomKwh + e.EnergieBelastingStroomKwh + e.VergroeningPrijsStroomKwh) * (1 + (e.BtwStroom / 100))
}

func (h *Handler) get() error {
	log.Printf("getting new stats")
	h.easyEnergy.m.Lock()
	defer h.easyEnergy.m.Unlock()

	h.easyEnergy.Tarief.GetData()

	if h.easyEnergy.Tarief.SpotPrijsStroomKwh == 0 {
		return fmt.Errorf("no spot price for power found yet")
	}

	log.Printf("tarief: %+v", h.easyEnergy.Tarief)
	return nil
}

func (h *Handler) put() {
	log.Printf("putting new stats")
	h.easyEnergy.m.Lock()
	defer h.easyEnergy.m.Unlock()

	h.statsd.Gauge(1.0, "easyenergy.spotprijsstroomkwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.SpotPrijsStroomKwh))
	log.Printf("sending value easyenergy.spotprijsstroomkwh=%f", h.easyEnergy.Tarief.SpotPrijsStroomKwh)

	h.statsd.Gauge(1.0, "easyenergy.spotprijsstroomterugkwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.SpotPrijsStroomTerugKwh))
	log.Printf("sending value easyenergy.spotprijsstroomterugkwh=%f", h.easyEnergy.Tarief.SpotPrijsStroomTerugKwh)
	h.statsd.Gauge(1.0, "easyenergy.opslageasyenergyprijsstroomkwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.OpslagEasyEnergyPrijsStroomKwh))
	log.Printf("sending value easyenergy.opslageasyenergyprijsstroomkwh=%f", h.easyEnergy.Tarief.OpslagEasyEnergyPrijsStroomKwh)
	h.statsd.Gauge(1.0, "easyenergy.vergroeningprijsstroomkwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.VergroeningPrijsStroomKwh))
	log.Printf("sending value easyenergy.vergroeningprijsstroomkwh=%f", h.easyEnergy.Tarief.VergroeningPrijsStroomKwh)

	// overheid
	h.statsd.Gauge(1.0, "easyenergy.energiebelasgingstroomkwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.EnergieBelastingStroomKwh))
	log.Printf("sending value easyenergy.energiebelasgingstroomkwh=%f", h.easyEnergy.Tarief.EnergieBelastingStroomKwh)
	h.statsd.Gauge(1.0, "easyenergy.opslagduurzameenergiestroomkwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.OpslagDuurzameEnergieStroomKwh))
	log.Printf("sending value easyenergy.opslagduurzameenergiestroomkwh=%f", h.easyEnergy.Tarief.OpslagDuurzameEnergieStroomKwh)
	h.statsd.Gauge(1.0, "easyenergy.btwstroom", fmt.Sprintf("%f", h.easyEnergy.Tarief.BtwStroom))
	log.Printf("sending value easyenergy.btwstroom=%f", h.easyEnergy.Tarief.BtwStroom)

	// levernacier
	h.statsd.Gauge(1.0, "easyenergy.spotprijsgasm3", fmt.Sprintf("%f", h.easyEnergy.Tarief.SpotPrijsGasM3))
	log.Printf("sending value easyenergy.spotprijsgasm3=%f", h.easyEnergy.Tarief.SpotPrijsGasM3)
	h.statsd.Gauge(1.0, "easyenergy.spotprijsgasterugm3", fmt.Sprintf("%f", h.easyEnergy.Tarief.SpotPrijsGasTerugM3))
	log.Printf("sending value easyenergy.spotprijsgasterugm3=%f", h.easyEnergy.Tarief.SpotPrijsGasTerugM3)
	h.statsd.Gauge(1.0, "easyenergy.opslageasyenergyprijsgaskwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.OpslagEasyEnergyPrijsGasM3))
	log.Printf("sending value easyenergy.opslageasyenergyprijsgaskwh=%f", h.easyEnergy.Tarief.OpslagEasyEnergyPrijsGasM3)

	// overheid
	h.statsd.Gauge(1.0, "easyenergy.opslagregioprijsgaskwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.OpslagRegioPrijsGasM3))
	log.Printf("sending value easyenergy.opslagregioprijsgaskwh=%f", h.easyEnergy.Tarief.OpslagRegioPrijsGasM3)
	h.statsd.Gauge(1.0, "easyenergy.energiebelasgingsgaskwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.EnergieBelastingGasM3))
	log.Printf("sending value easyenergy.energiebelasgingsgaskwh=%f", h.easyEnergy.Tarief.EnergieBelastingGasM3)
	h.statsd.Gauge(1.0, "easyenergy.opslagduurzameenergiesgaskwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.OpslagDuurzameEnergieGasM3))
	log.Printf("sending value easyenergy.opslagduurzameenergiesgaskwh=%f", h.easyEnergy.Tarief.OpslagDuurzameEnergieGasM3)
	h.statsd.Gauge(1.0, "easyenergy.btwgas", fmt.Sprintf("%f", h.easyEnergy.Tarief.BtwGas))
	log.Printf("sending value easyenergy.btwgas=%f", h.easyEnergy.Tarief.BtwGas)

	h.statsd.Gauge(1.0, "easyenergy.totalprijsstroomkwh", fmt.Sprintf("%f", h.easyEnergy.Tarief.TotalPrijsStroomKwh))
	log.Printf("sending value easyenergy.totalprijsstroomkwh=%f", h.easyEnergy.Tarief.TotalPrijsStroomKwh)
	h.statsd.Gauge(1.0, "easyenergy.totalprijsgasm3", fmt.Sprintf("%f", h.easyEnergy.Tarief.TotalPrijsGasM3))
	log.Printf("sending value easyenergy.totalprijsgasm3=%f", h.easyEnergy.Tarief.TotalPrijsGasM3)

}

func main() {

	h := Handler{
		statsd: statsdhelper.New(),
		easyEnergy: &EasyEnergy{
			Tarief: EasyEnergyTarief{},
		},
	}

	getTicker := time.NewTicker(60 * time.Minute)
	putTicker := time.NewTicker(60 * time.Second)

	// loop till exit
	sigterm := make(chan os.Signal, 10)
	signal.Notify(sigterm, os.Interrupt, syscall.SIGTERM)

	// original get
	h.get()

	for {
		select {
		case <-sigterm:
			log.Printf("Program killed by signal!")
			return
		case <-getTicker.C:
			h.get()
		case <-putTicker.C:
			h.put()
		}
	}
}
