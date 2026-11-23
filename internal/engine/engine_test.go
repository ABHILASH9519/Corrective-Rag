package engine

import("testing")
func TestBM25Retrieval(t *testing.T){e:=&Engine{cfg:structConfig(),docs:[]Passage{{ID:"rail",Text:"Tatkal booking opens at ten AM for AC classes"},{ID:"bag",Text:"Passengers may carry luggage on a train"}}};got:=e.retrieve("Tatkal AC booking time");if len(got)==0||got[0].ID!="rail"{t.Fatalf("expected rail passage first, got %#v",got)}}
func structConfig()config.Config{return config.Config{TopK:2}}
