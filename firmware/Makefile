# Firmware for the Heltec WiFi LoRa 32 (V3), built with the Arduino IDE's CLI.
ARDUINO_CLI ?= /Applications/Arduino IDE.app/Contents/Resources/app/lib/backend/resources/arduino-cli
FQBN        ?= esp32:esp32:heltec_wifi_lora_32_V3
PORT        ?= $(firstword $(wildcard /dev/cu.usbserial-* /dev/cu.SLAB_USBtoUART*))
SKETCH      := caycle_esp32

.PHONY: test build flash monitor

# Unit tests for the pure logic (FE-C, workouts, button) on the host.
test:
	c++ -std=c++17 -Wall -Wextra -Werror -o test/run test/test_main.cpp \
		$(SKETCH)/fec.cpp $(SKETCH)/workout.cpp $(SKETCH)/button.cpp $(SKETCH)/ridefile.cpp $(SKETCH)/json.cpp
	./test/run
	python3 -I -c 'import sys, xml.dom.minidom; xml.dom.minidom.parse(sys.argv[1]); print("TCX is well-formed XML")' test/ride.tcx

build:
	"$(ARDUINO_CLI)" compile --fqbn $(FQBN) $(SKETCH)

flash: build
	"$(ARDUINO_CLI)" upload --fqbn $(FQBN) --port $(PORT) $(SKETCH)

monitor:
	"$(ARDUINO_CLI)" monitor --port $(PORT) --config baudrate=115200
