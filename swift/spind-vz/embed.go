package spindvz

import _ "embed"

//go:embed main.swift
var MainSwift string

//go:embed spind-vz.entitlements
var Entitlements string
