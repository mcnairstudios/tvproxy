package tvsatipscan

import (
	"sort"
	"strings"
)

// Satellites returns a sorted list of supported satellite identifiers.
func Satellites() []string {
	keys := make([]string, 0, len(europeanSatellites))
	for k := range europeanSatellites {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func satelliteList() string {
	return strings.Join(Satellites(), ", ")
}

// satelliteDetectionSeeds has one well-known transponder per satellite used for
// auto-detection. All satellites are probed in parallel; the first NIT response
// identifies the satellite. BFS then uses the full europeanSatellites seed list.
var satelliteDetectionSeeds = map[string]Transponder{
	"S28.2E": {FreqMHz: 10714, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	"S19.2E": {FreqMHz: 10773, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	"S13E":   {FreqMHz: 11200, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	"S0.8W":  {FreqMHz: 11012, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "h"},
	"S30W":   {FreqMHz: 11678, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
}

// europeanSatellites maps satellite identifiers to representative seed transponders.
// Identifiers follow the w_scan2 convention: S<degrees><E|W>.
//
// Seeds are spread across both polarisations and both frequency bands so that at
// least one will lock regardless of local signal conditions. Once any seed locks,
// NIT BFS discovers all remaining transponders on the satellite automatically —
// these lists do not need to be exhaustive.
var europeanSatellites = map[string][]Transponder{
	"S28.2E": astra28E,   // UK / Ireland — Sky, Freesat, BBC, ITV
	"S19.2E": astra19E,   // Germany, Austria, Switzerland, Netherlands — ARD, ZDF, RTL, Sky DE
	"S13E":   hotbird13E, // Pan-European — RAI, TF1, France Télévisions, many Eastern European
	"S0.8W":  thor08W,    // Scandinavia — Canal Digital, TV2, SVT, NRK
	"S30W":   hispasat30W, // Spain, Portugal, Latin America — RTVE, RTP, Antena 3
}

// astra28E — SES Astra 2 at 28.2°E
// Primary UK/Ireland satellite. Carries Sky UK, Freesat, BBC, ITV, Channel 4, Channel 5.
// Low band: 10.7–11.7 GHz, 22000 kS/s, DVB-S2 QPSK
// High band: 11.7–12.75 GHz, 27500 kS/s, DVB-S2 QPSK
var astra28E = []Transponder{
	// Low band, horizontal
	{FreqMHz: 10714, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10803, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10891, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	// Low band, vertical
	{FreqMHz: 10729, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10818, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10906, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	// High band, horizontal
	{FreqMHz: 11222, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11538, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11856, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	// High band, vertical
	{FreqMHz: 11302, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11597, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11895, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
}

// astra19E — SES Astra 1 at 19.2°E
// Primary satellite for Germany, Austria, Switzerland, Netherlands, Belgium.
// Carries ARD, ZDF, RTL, ProSieben, Sat.1, Sky Deutschland, Canal+, and many others.
// Low band: 22000 kS/s DVB-S2; high band: 27500 kS/s DVB-S/S2 mixed.
var astra19E = []Transponder{
	// Low band, horizontal
	{FreqMHz: 10773, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10832, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10921, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 11009, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	// Low band, vertical
	{FreqMHz: 10744, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10818, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10906, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10994, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	// High band, horizontal
	{FreqMHz: 11229, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11347, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11479, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11720, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11836, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	// High band, vertical
	{FreqMHz: 11278, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11414, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11538, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11778, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
}

// hotbird13E — Eutelsat Hot Bird at 13°E
// Pan-European satellite visible across Europe, Middle East, and North Africa.
// Carries RAI (Italy), TF1/France Télévisions, many Eastern European national broadcasters.
// Primarily 27500 kS/s DVB-S/S2.
var hotbird13E = []Transponder{
	// Horizontal
	{FreqMHz: 10873, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 10971, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11096, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11200, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11325, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11449, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11604, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11727, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	// Vertical
	{FreqMHz: 10930, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11033, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11137, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11242, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11370, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11491, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11623, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11766, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
}

// thor08W — Thor 6/7 at 0.8°W (Telenor/Intelsat)
// Primary satellite for Scandinavia. Carries Canal Digital (NO/SE/DK/FI), TV2, SVT, NRK, DR.
// Uses mixed symbol rates: 24500 and 28000 kS/s, DVB-S2.
var thor08W = []Transponder{
	// Horizontal
	{FreqMHz: 11012, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "h"},
	{FreqMHz: 11054, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 28000, Polarization: "h"},
	{FreqMHz: 11117, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 28000, Polarization: "h"},
	{FreqMHz: 11179, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "h"},
	{FreqMHz: 11242, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "h"},
	// Vertical
	{FreqMHz: 11075, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 28000, Polarization: "v"},
	{FreqMHz: 11138, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 28000, Polarization: "v"},
	{FreqMHz: 11200, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "v"},
	{FreqMHz: 11263, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "v"},
}

// hispasat30W — Hispasat 30W-6 at 30°W
// Primary satellite for Spain and Portugal. Also serves Latin America.
// Carries RTVE, Antena 3, Telecinco, RTP, SIC, and regional Spanish channels.
// Primarily 27500 kS/s DVB-S2.
var hispasat30W = []Transponder{
	// Horizontal
	{FreqMHz: 11594, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11678, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11762, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11856, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11938, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 12054, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	// Vertical
	{FreqMHz: 11638, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11722, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11810, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11977, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
}
