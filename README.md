# Specialised-Neural-Network


The code of this repo implements an end-to-end Go system, combining custom Gonum neural networks, fuzzy agentic schema repair, strict row validation, and a clean HTTP pipeline. It also has an endpoint for downloading the validated/changed CSVs, in order to be integrated for further automation in data pipelines. 

It's purpose is to be a Neural Network with an agentic layer for CSV Data Validation. (a small prototype for now)

Overall, following validation and catching rules per rows in a CSV are implemented: <br>
✅ duplicate ids <br>
✅ invalid email format <br>
✅ wrong date format <br>
✅ out of range numbers in string columns <br>

Project Structure 
```
# tree .
.
├── 4curl
│   ├── example1.csv
│   ├── example2.csv
│   └── example3.csv
├── Dockerfile
├── README.md
├── agent
│   ├── go.mod
│   ├── schema-agent.go
│   └── schema_agent_test.go
├── dataset.csv
├── go.mod
├── go.sum
├── main.go
├── model
│   ├── go.mod
│   ├── go.sum
│   ├── header_validator.go
│   ├── type_validator.go
│   └── validator_test.go
└── server
    ├── go.mod
    ├── handlers.go
    └── handlers_test.go

4 directories, 20 files
```

<b><i> The pattern recognition for your CSVs you need to validate is done with the help of dataset.csv file </i></b>

<br>
</br>

<i> How to run it </i> : it's all entirely developed in Go, so you just "go run main.go" 
(I do presume you know how to use "go mod" & "go tidy" commands... )

---

<b> Example how to use it </b>

1) Straight out of the box

Start it as a background process:

```
# go run main.go  &
[1] 17879
 Training Header Neural Network on columns: [customer_id customer_name city age account_balance email date]
column rules infer: map[account_balance:{numeric 0x141e737ac450 0x141e737ac458  false} age:{numeric 0x141e737ac320 0x141e737ac328  false} city:{string <nil> <nil>  false} customer_id:{numeric 0x141e73637e40 <nil>  true} customer_name:{string <nil> <nil>  false} date:{date <nil> <nil> 2006-01-02 false} email:{email <nil> <nil>  false}]
Saved header validator weights to header_validator.json
2026/11/12 14:42:58 CSV Validator running on http://localhost:8080/validate/csv
```

Under folder 4curl/ you have a few CSVs for testing:
```
# ls 4curl/
example1.csv  example2.csv  example3.csv
```

example1.csv has two columns wrongly typed. 
Let's see if the nn can find them:
```
4# curl -X POST http://localhost:8080/validate/csv  -F "file=@4curl/example1.csv"
{"status":"REQUIRES_CONFIRMATION","is_valid":false,"detected_headers":["customer_id","custmer_name","city","age","account_balnce","email","date"],"column_scores":[{"column":"customer_id","recognized":true,"confidence":0.9434328990974324},{"column":"custmer_name","recognized":false,"confidence":0.00014154973216475724},
{"column":"city","recognized":true,"confidence":0.9699381526992603},{"column":"age","recognized":true,"confidence":0.9823877829235843},
{"column":"account_balnce","recognized":false,"confidence":5.836974438225906e-7},{"column":"email","recognized":true,"confidence":0.9917857748710058},
{"column":"date","recognized":true,"confidence":0.9378697031184884}],"session_id":"ad0182708afbbe74fcfcb75b","message":"Agent: Found 2 mismatched columns. Do you want to
apply these renames: 'account_balnce' -\u003e 'account_balance', 'custmer_name' -\u003e 'customer_name'?","duration":"534.115µs"}
```

... and confirm to change the wrongly typed columns ( you will need the session ID):

```
kek #  curl -X POST http://localhost:8080/validate/confirm \
> -H "Content-Type: application/json" \
> -d '{"session_id": "ad0182708afbbe74fcfcb75b", "approved": true}' 
{"status":"VALID","is_valid":true,"detected_headers":["customer_id","customer_name","city","age","account_balance","email","date"],"column_scores":[{"column":"customer_id","recognized":true,"confidence":0.9434328990974324},{"column":"customer_name","recognized":true,"confidence":0.9198644088832673},
{"column":"city","recognized":true,"confidence":0.9699381526992603},{"column":"age","recognized":true,"confidence":0.9823877829235843},
{"column":"account_balance","recognized":true,"confidence":0.9493188629584499},{"column":"email","recognized":true,"confidence":0.9917857748710058},
{"column":"date","recognized":true,"confidence":0.9378697031184884}],"saved_file_path":"persisted_csvs/cleaned_1794487944_example1.csv","updated_csv":"customer_id,customer_n
ame,city,age,account_balance,email,date\n109,Dinu,Berlin,31,2100.00,dinu@example.com,2012-12-12\n111,Karl,Sankt Petersburg,28,765.40,Karl@example.com,2011-11-
11\n","message":"CSV schema and row data types validated successfully.","duration":"873.676µs"}
```

After this step, the project structure looks as below (there is a new folder created for the changed/fixed CSVs, persisted_csvs/):

```
tree .
.
├── 4curl
│   ├── example1.csv
│   ├── example2.csv
│   └── example3.csv
├── Dockerfile
├── README.md
├── agent
│   ├── go.mod
│   ├── schema-agent.go
│   └── schema_agent_test.go
├── dataset.csv
├── go.mod
├── go.sum
├── header_validator.json
├── learned_rules.json
├── main.go
├── model
│   ├── go.mod
│   ├── go.sum
│   ├── header_validator.go
│   ├── type_validator.go
│   └── validator_test.go
├── persisted_csvs
│   └── cleaned_1794487944_example1.csv
└── server
    ├── go.mod
    ├── handlers.go
    └── handlers_test.go

5 directories, 23 files

```
And let's check the content of the validated CSV:
```
# more persisted_csvs/cleaned_1794487944_example1.csv
customer_id,customer_name,city,age,account_balance,email,date
109,Dinu,Berlin,31,2100.00,dinu@example.com,2012-12-12
111,Karl,Sankt Petersburg,28,765.40,Karl@example.com,2011-11-11
```


2) Containerized solution

You can also deploy it on a Docker container and if you like, deploy it on Kubernetes (but the manifests are not here, you will need to create them). You need to take care of the storage volumes even for the Docker.

There is also a Dockerfile in this project, and once the image is built, you will notice it is quite minimal, barely 15mb in size. 

Example: <br>
<i> n.b: below example has no volume mounts for docker container! This is homework for you.  
And yes, if you put it on k8s, you need persistent storage too, to make it fully functionable. </i>


```
kek# docker build -t neural-network-validator .
<... snip ... >
kek# #check image
kek# docker images | grep neural
neural-network-validator   latest    bbbf89cccxx   About an hour ago   14.9MB
kek # # build a detached container that will validate a local file of your machine
kek #  docker run -p 9055:8080 -d  neural-network-validator:latest
< ... snip ... >
kek # docker ps
CONTAINER ID   IMAGE                             COMMAND                  CREATED         STATUS         PORTS                                         NAMES
9999855967   neural-network-validator:latest   "/app/csv-neural-net…"   8 seconds ago   Up 7 seconds   0.0.0.0:9055->8080/tcp, [::]:9055->8080/tcp   heuristic_smiork
k3k # # and check if it can find the duplicates from 4curl/example2.csv
curl -X POST http://0.0.0.0:9055/validate/csv  -F "file=@4curl/example3.csv"
{"status":"TYPE_VALIDATION_FAILED","is_valid":false,"detected_headers":["customer_id","customer_name","city","age","account_balance","email","date"],"column_scores":
[{"column":"customer_id","recognized":true,"confidence":0.949176765568842},{"column":"customer_name","recognized":true,"confidence":0.9365634078249371},
{"column":"city","recognized":true,"confidence":0.9625704828814533},{"column":"age","recognized":true,"confidence":0.9751262962027623},
{"column":"account_balance","recognized":true,"confidence":0.8831511683094623},{"column":"email","recognized":true,"confidence":0.9848383555624355},
{"column":"date","recognized":true,"confidence":0.9309909638641}],"type_errors":[{"row":2,"column":"customer_id","value":"111","reason":"duplicate value '111' in unique
 column 'customer_id' (first seen at row 1)"}],"message":"Found 1 row type validation error(s).","duration":"403.888µs"}
```

<br>
</br>
<b> n.b: The cached model is in header_validator.json, so you need to delete it for retraining. </b>
<br>
</br>

<i>Fini!</i>
