package config

import("os";"strconv";"strings")
type Config struct{Address,Model,APIKey,APIBase,Token,Origins,KBPath,EvalPath string;TopK,MaxQuestion int}
func val(k,d string)string{if v:=strings.TrimSpace(os.Getenv(k));v!=""{return v};return d}
func num(k string,d int)int{n,e:=strconv.Atoi(os.Getenv(k));if e==nil&&n>0{return n};return d}
func Load()Config{return Config{Address:val("CRAG_ADDR",":8080"),Model:val("CRAG_MODEL","openai/gpt-oss-120b"),APIKey:val("GROQ_API_KEY",""),APIBase:strings.TrimRight(val("GROQ_API_BASE","https://api.groq.com/openai/v1"),"/"),Token:val("CRAG_API_TOKEN",""),Origins:val("CRAG_ALLOWED_ORIGINS",""),KBPath:val("CRAG_KB_PATH","data/knowledge_base.json"),EvalPath:val("CRAG_EVAL_PATH","data/evaluation_set.json"),TopK:num("CRAG_TOP_K",5),MaxQuestion:num("CRAG_MAX_QUESTION_CHARS",2000)}}
