package main

// The Task Scheduler definition and its UTF-16 encoding live here, deliberately WITHOUT
// a build tag. They are pure string handling, and keeping them out of the windows-only
// file is what lets CI assert them on Linux — including that the 72-hour execution
// limit really is lifted, a failure that would otherwise surface silently three days
// after an install.

import (
	"bytes"
	"encoding/xml"
	"strings"
	"text/template"
	"unicode/utf16"
)

const taskName = "anjin-intel"

// taskXML is the Task Scheduler definition. Four of its settings exist purely to undo
// defaults that would silently stop a long-running shipper:
//
//   - ExecutionTimeLimit defaults to PT72H, after which the task is KILLED. PT0S is
//     unlimited. Without it the shipper dies every three days — a failure three days
//     removed from its cause, which is why it is asserted in a test rather than trusted.
//   - DisallowStartIfOnBatteries defaults true: on a laptop it would never start on
//     battery.
//   - StopIfGoingOnBatteries defaults true: it would stop the moment you unplug.
//   - MultipleInstancesPolicy IgnoreNew stops a re-login running two shippers against
//     one log directory.
//
// Element order inside <Settings> is not free — the schema declares a sequence, and a
// task whose elements are out of order is rejected outright.
//
// UserId is pinned on both the principal and the trigger: registration happens through
// an elevated process, and leaving the owner implicit invites the task binding to the
// wrong identity. RunLevel stays LeastPrivilege — registering elevated must not make it
// RUN elevated, or it would read the wrong profile's Chatlogs.
var taskXML = template.Must(template.New("task").Funcs(template.FuncMap{
	"x": xmlEscape,
}).Parse(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>anjin-intel — EVE chat-intel shipper</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>{{x .User}}</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>{{x .User}}</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <Enabled>true</Enabled>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>"{{x .Bin}}"</Command>
      <Arguments>run --background</Arguments>
    </Exec>
  </Actions>
</Task>
`))

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// renderTaskXML builds the task definition for bin, owned by user.
func renderTaskXML(bin, user string) (string, error) {
	var b strings.Builder
	if err := taskXML.Execute(&b, struct{ Bin, User string }{bin, user}); err != nil {
		return "", err
	}
	return b.String(), nil
}

// utf16LEWithBOM encodes s as UTF-16LE with a leading BOM. schtasks demands this: an
// XML file whose declaration says encoding="UTF-16" must really be UTF-16LE *with* the
// 0xFF 0xFE bytes, or it is rejected with the memorably unhelpful
// "The task XML is malformed. (1,2)::ERROR: one root element".
// (Never a raw BOM in Go source — the escape, as with the chatlog decoder.)
func utf16LEWithBOM(s string) []byte {
	u := utf16.Encode([]rune("\ufeff" + s))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}
