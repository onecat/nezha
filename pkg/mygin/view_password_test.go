package mygin

import ("testing";"time")

func TestViewPasswordToken(t *testing.T) {
	now:=time.Now()
	token:=NewViewPasswordToken("test-password",now)
	if !validViewPasswordToken(token,"test-password",now) {t.Fatal("valid token rejected")}
	if validViewPasswordToken(token,"changed",now) {t.Fatal("changed password accepted")}
	if validViewPasswordToken(token,"test-password",now.Add(ViewPasswordAge)) {t.Fatal("expired token accepted")}
	if validViewPasswordToken(token+"x","test-password",now) {t.Fatal("malformed token accepted")}
	if validViewPasswordToken("invalid","test-password",now) {t.Fatal("invalid cookie accepted")}
}
