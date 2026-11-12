module main

go 1.27.4

replace server => ./server

replace agent => ./agent

replace model => ./model

require (
	model v0.0.0-00010101000000-000000000000
	server v0.0.0-00010101000000-000000000000
)

require (
	agent v0.0.0-00010101000000-000000000000 // indirect
	gonum.org/v1/gonum v0.17.0 // indirect
)
