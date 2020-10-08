package main

import (
	"os"
	"fmt"
	"log"
	"time"
	"bytes"
	"context"
	"strings"
	"reflect"
	"net/http"
	"encoding/json"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/serverlessapplicationrepository"
)

type Application struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Stack       Stack  `json:"stack"`
}

type Stack struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Url    string `json:"url"`
}

type APIResponse struct {
	Message         string        `json:"message"`
	ApplicationList []Application `json:"applicationList"`
}

type Response events.APIGatewayProxyResponse

var cfg aws.Config
var cloudformationClient *cloudformation.Client
var serverlessApplicationRepositoryClient *serverlessapplicationrepository.Client

const layout string = "20060102150405.000"

func HandleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (Response, error) {
	var jsonBytes []byte
	var err error
	d := make(map[string]string)
	json.Unmarshal([]byte(request.Body), &d)
	if v, ok := d["action"]; ok {
		switch v {
		case "status" :
			l, e := getApplications(ctx)
			if e != nil {
				err = e
			} else {
				jsonBytes, _ = json.Marshal(APIResponse{Message: "Success.", ApplicationList: l})
			}
		case "create" :
			if n, ok := d["name"]; ok {
				e := createStack(ctx, n)
				if e != nil {
					err = e
				} else {
					jsonBytes, _ = json.Marshal(APIResponse{Message: "Success.", ApplicationList: nil})
				}
			}
		case "delete" :
			if n, ok := d["name"]; ok {
				e := deleteStack(ctx, n)
				if e != nil {
					err = e
				} else {
					jsonBytes, _ = json.Marshal(APIResponse{Message: "Success.", ApplicationList: nil})
				}
			}
		}
	}
	log.Print(request.RequestContext.Identity.SourceIP)
	if err != nil {
		log.Print(err)
		jsonBytes, _ = json.Marshal(APIResponse{Message: fmt.Sprint(err)})
		return Response{
			StatusCode: http.StatusInternalServerError,
			Body: string(jsonBytes),
		}, nil
	}
	return Response {
		StatusCode: http.StatusOK,
		Body: string(jsonBytes),
	}, nil
}

func getApplications(ctx context.Context)([]Application, error) {
	if serverlessApplicationRepositoryClient == nil {
		serverlessApplicationRepositoryClient = getSARClient()
	}
	res, err := serverlessApplicationRepositoryClient.ListApplications(ctx, &serverlessapplicationrepository.ListApplicationsInput{})
	if err != nil {
		return nil, err
	}
	var applicationList []Application
	for _, i := range res.Applications {
		applicationList = append(applicationList, Application{
			Name:        stringValue(i.Name),
			Description: stringValue(i.Description),
			Stack:       Stack{},
		})
	}
	applicationList, err = addStackData(ctx, applicationList)
	if err != nil {
		return nil, err
	}
	return applicationList, nil
}

func getApplicationId(ctx context.Context, name string)(string, error) {
	if serverlessApplicationRepositoryClient == nil {
		serverlessApplicationRepositoryClient = getSARClient()
	}
	res, err := serverlessApplicationRepositoryClient.ListApplications(ctx, &serverlessapplicationrepository.ListApplicationsInput{})
	if err != nil {
		return "", err
	}
	var applicationId string
	for _, i := range res.Applications {
		if name == stringValue(i.Name) {
			applicationId = stringValue(i.ApplicationId)
			break
		}
	}
	return applicationId, nil
}

func getTemplateUrl(ctx context.Context, applicationId string)(string, error) {
	if serverlessApplicationRepositoryClient == nil {
		serverlessApplicationRepositoryClient = getSARClient()
	}
	res, err := serverlessApplicationRepositoryClient.CreateCloudFormationTemplate(ctx, &serverlessapplicationrepository.CreateCloudFormationTemplateInput{
		ApplicationId: aws.String(applicationId),
	})
	if err != nil {
		return "", err
	}
	return stringValue(res.TemplateUrl), nil
}

func addStackData(ctx context.Context, applicationList []Application)([]Application, error) {
	if cloudformationClient == nil {
		cloudformationClient = getCloudformationClient()
	}
	res, err := cloudformationClient.ListStacks(ctx, &cloudformation.ListStacksInput{
		StackStatusFilter: []types.StackStatus{
			types.StackStatusCreate_complete,
			types.StackStatusCreate_in_progress,
			types.StackStatusDelete_in_progress,
		},
	})
	if err != nil {
		return nil, err
	}
	for _, i := range res.StackSummaries {
		stackName := stringValue(i.StackName)
		for n, j := range applicationList {
			if strings.HasPrefix(stackName, j.Name) {
				var url string
				if i.StackStatus == types.StackStatusCreate_complete{
					res_, err := cloudformationClient.ListStackResources(ctx, &cloudformation.ListStackResourcesInput{StackName: i.StackName})
					if err != nil {
						log.Println(err)
						break
					}
					for _, j := range res_.StackResourceSummaries {
						if stringValue(j.ResourceType) == "AWS::ApiGatewayV2::Api" {
							url = "https://" + stringValue(j.PhysicalResourceId) + ".execute-api." + os.Getenv("REGION") + ".amazonaws.com/"
						}
					}
				}
				applicationList[n].Stack = Stack{Name: stackName, Status: string(i.StackStatus), Url: url}
				break
			}
		}
	}
	return applicationList, nil
}

func createStack(ctx context.Context, name string) error {
	applicationId, err := getApplicationId(ctx, name)
	if err != nil {
		log.Println(err)
		return err
	}
	templateUrl, err := getTemplateUrl(ctx, applicationId)
	if err != nil {
		log.Println(err)
		return err
	}
	t := time.Now()
	stackName := name + strings.Replace(t.Format(layout), ".", "", 1)
	if cloudformationClient == nil {
		cloudformationClient = getCloudformationClient()
	}
	_, err = cloudformationClient.CreateStack(ctx, &cloudformation.CreateStackInput{
		Capabilities: []types.Capability{
			types.CapabilityCapability_iam,
			types.CapabilityCapability_auto_expand,
		},
		StackName: aws.String(stackName),
		TemplateURL: aws.String(templateUrl),
	})
	if err != nil {
		log.Println(err)
		return err
	}
	return nil
}

func deleteStack(ctx context.Context, name string) error {
	if cloudformationClient == nil {
		cloudformationClient = getCloudformationClient()
	}
	_, err := cloudformationClient.DeleteStack(ctx, &cloudformation.DeleteStackInput{
		StackName: aws.String(name),
	})
	if err != nil {
		log.Println(err)
	}
	return nil
}

func getTargetStack(name string, list []Stack) Stack {
	var stack Stack
	for _, i := range list {
		if i.Name == name {
			stack = i
			break
		}
	}
	return stack
}

func getSARClient() *serverlessapplicationrepository.Client {
	if cfg.Region != os.Getenv("REGION") {
		cfg = getConfig()
	}
	return serverlessapplicationrepository.NewFromConfig(cfg)
}

func getCloudformationClient() *cloudformation.Client {
	if cfg.Region != os.Getenv("REGION") {
		cfg = getConfig()
	}
	return cloudformation.NewFromConfig(cfg)
}

func getConfig() aws.Config {
	var err error
	newConfig, err := config.LoadDefaultConfig()
	newConfig.Region = os.Getenv("REGION")
	if err != nil {
		log.Print(err)
	}
	return newConfig
}

func stringValue(i interface{}) string {
	var buf bytes.Buffer
	strVal(reflect.ValueOf(i), 0, &buf)
	res := buf.String()
	return res[1:len(res) - 1]
}

func strVal(v reflect.Value, indent int, buf *bytes.Buffer) {
	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Struct:
		buf.WriteString("{\n")
		for i := 0; i < v.Type().NumField(); i++ {
			ft := v.Type().Field(i)
			fv := v.Field(i)
			if ft.Name[0:1] == strings.ToLower(ft.Name[0:1]) {
				continue // ignore unexported fields
			}
			if (fv.Kind() == reflect.Ptr || fv.Kind() == reflect.Slice) && fv.IsNil() {
				continue // ignore unset fields
			}
			buf.WriteString(strings.Repeat(" ", indent+2))
			buf.WriteString(ft.Name + ": ")
			if tag := ft.Tag.Get("sensitive"); tag == "true" {
				buf.WriteString("<sensitive>")
			} else {
				strVal(fv, indent+2, buf)
			}
			buf.WriteString(",\n")
		}
		buf.WriteString("\n" + strings.Repeat(" ", indent) + "}")
	case reflect.Slice:
		nl, id, id2 := "", "", ""
		if v.Len() > 3 {
			nl, id, id2 = "\n", strings.Repeat(" ", indent), strings.Repeat(" ", indent+2)
		}
		buf.WriteString("[" + nl)
		for i := 0; i < v.Len(); i++ {
			buf.WriteString(id2)
			strVal(v.Index(i), indent+2, buf)
			if i < v.Len()-1 {
				buf.WriteString("," + nl)
			}
		}
		buf.WriteString(nl + id + "]")
	case reflect.Map:
		buf.WriteString("{\n")
		for i, k := range v.MapKeys() {
			buf.WriteString(strings.Repeat(" ", indent+2))
			buf.WriteString(k.String() + ": ")
			strVal(v.MapIndex(k), indent+2, buf)
			if i < v.Len()-1 {
				buf.WriteString(",\n")
			}
		}
		buf.WriteString("\n" + strings.Repeat(" ", indent) + "}")
	default:
		format := "%v"
		switch v.Interface().(type) {
		case string:
			format = "%q"
		}
		fmt.Fprintf(buf, format, v.Interface())
	}
}

func main() {
	lambda.Start(HandleRequest)
}
