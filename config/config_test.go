package config

import (
	"os"
	"testing"
)

func TestGet(t *testing.T) {
	err := os.Setenv("JOBRUNNER_PORT", "1337")
	if err != nil {
		t.Errorf(err.Error())
	}
	err = os.Setenv("JOBRUNNER_HOSTNAME", "localhost.localdomain")
	if err != nil {
		t.Errorf(err.Error())
	}
	err = os.Setenv("JOBRUNNER_BASE", "/tmp/")
	if err != nil {
		t.Errorf(err.Error())
	}
	Configure()
	cfg := Get()
	if cfg.Host != "localhost.localdomain:1337" {
		t.Errorf("Host was set to %s", cfg.Host)
	}
	if cfg.BaseDir != "/tmp/" {
		t.Errorf("Base directory was set to %s", cfg.BaseDir)
	}
}

func TestSet(t *testing.T) {
	err := os.Setenv("JOBRUNNER_PORT", "1337")
	if err != nil {
		t.Errorf(err.Error())
	}
	err = os.Setenv("JOBRUNNER_HOSTNAME", "localhost.localdomain")
	if err != nil {
		t.Errorf(err.Error())
	}
	err = os.Setenv("JOBRUNNER_BASE", "/tmp/")
	if err != nil {
		t.Errorf(err.Error())
	}
	Configure()
	newvalues := ConfigValues{
		Host:    "lol:0000",
		BaseDir: "/tmp2/",
	}
	Set(newvalues)
	cfg := Get()
	if cfg.Host != "lol:0000" {
		t.Errorf("Host was set to %s", cfg.Host)
	}
	if cfg.BaseDir != "/tmp2/" {
		t.Errorf("Base directory was set to %s", cfg.BaseDir)
	}

}
