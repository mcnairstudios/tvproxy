package tvsatipscan

import (
	"sort"
	"strings"
)

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

var satelliteDetectionSeeds = map[string]Transponder{
	"S28.2E": {FreqMHz: 10714, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	"S19.2E": {FreqMHz: 10773, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	"S13E":   {FreqMHz: 11200, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	"S0.8W":  {FreqMHz: 11012, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "h"},
	"S30W":   {FreqMHz: 11678, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
}

var europeanSatellites = map[string][]Transponder{
	"S28.2E": astra28E,
	"S19.2E": astra19E,
	"S13E":   hotbird13E,
	"S0.8W":  thor08W,
	"S30W":   hispasat30W,
}

var astra28E = []Transponder{
	{FreqMHz: 10714, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10803, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10891, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10729, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10818, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10906, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 11222, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11538, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11856, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11302, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11597, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11895, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
}

var astra19E = []Transponder{
	{FreqMHz: 10773, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10832, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10921, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 11009, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "h"},
	{FreqMHz: 10744, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10818, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10906, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 10994, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 22000, Polarization: "v"},
	{FreqMHz: 11229, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11347, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11479, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11720, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11836, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11278, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11414, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11538, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11778, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
}

var hotbird13E = []Transponder{
	{FreqMHz: 10873, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 10971, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11096, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11200, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11325, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11449, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11604, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11727, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 10930, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11033, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11137, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11242, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11370, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11491, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11623, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11766, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
}

var thor08W = []Transponder{
	{FreqMHz: 11012, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "h"},
	{FreqMHz: 11054, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 28000, Polarization: "h"},
	{FreqMHz: 11117, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 28000, Polarization: "h"},
	{FreqMHz: 11179, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "h"},
	{FreqMHz: 11242, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "h"},
	{FreqMHz: 11075, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 28000, Polarization: "v"},
	{FreqMHz: 11138, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 28000, Polarization: "v"},
	{FreqMHz: 11200, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "v"},
	{FreqMHz: 11263, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 24500, Polarization: "v"},
}

var hispasat30W = []Transponder{
	{FreqMHz: 11594, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11678, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11762, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11856, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11938, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 12054, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	{FreqMHz: 11638, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11722, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11810, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
	{FreqMHz: 11977, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "v"},
}
