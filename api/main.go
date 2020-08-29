package main

import (
	"os"
	"fmt"
	"log"
	"time"
	"context"
	"strings"
	"net/http"
	"encoding/json"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
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
		serverlessApplicationRepositoryClient = serverlessapplicationrepository.New(cfg)
	}
	req := serverlessApplicationRepositoryClient.ListApplicationsRequest(&serverlessapplicationrepository.ListApplicationsInput{})
	res, err := req.Send(ctx)
	if err != nil {
		return nil, err
	}
	var applicationList []Application
	for _, i := range res.ListApplicationsOutput.Applications {
		applicationList = append(applicationList, Application{
			Name:        aws.StringValue(i.Name),
			Description: aws.StringValue(i.Description),
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
		serverlessApplicationRepositoryClient = serverlessapplicationrepository.New(cfg)
	}
	req := serverlessApplicationRepositoryClient.ListApplicationsRequest(&serverlessapplicationrepository.ListApplicationsInput{})
	res, err := req.Send(ctx)
	if err != nil {
		return "", err
	}
	var applicationId string
	for _, i := range res.ListApplicationsOutput.Applications {
		if name == aws.StringValue(i.Name) {
			applicationId = aws.StringValue(i.ApplicationId)
			break
		}
	}
	return applicationId, nil
}

func getTemplateUrl(ctx context.Context, applicationId string)(string, error) {
	if serverlessApplicationRepositoryClient == nil {
		serverlessApplicationRepositoryClient = serverlessapplicationrepository.New(cfg)
	}
	req := serverlessApplicationRepositoryClient.CreateCloudFormationTemplateRequest(&serverlessapplicationrepository.CreateCloudFormationTemplateInput{
		ApplicationId: aws.String(applicationId),
	})
	res, err := req.Send(ctx)
	if err != nil {
		return "", err
	}
	return aws.StringValue(res.CreateCloudFormationTemplateOutput.TemplateUrl), nil
}

func addStackData(ctx context.Context, applicationList []Application)([]Application, error) {
	if cloudformationClient == nil {
		cloudformationClient = cloudformation.New(cfg)
	}
	req := cloudformationClient.ListStacksRequest(&cloudformation.ListStacksInput{
		StackStatusFilter: []cloudformation.StackStatus{
			cloudformation.StackStatusCreateComplete,
			cloudformation.StackStatusCreateInProgress,
			cloudformation.StackStatusDeleteInProgress,
		},
	})
	res, err := req.Send(ctx)
	if err != nil {
		return nil, err
	}
	for _, i := range res.ListStacksOutput.StackSummaries {
		stackName := aws.StringValue(i.StackName)
		for n, j := range applicationList {
			if strings.HasPrefix(stackName, j.Name) {
				var url string
				if i.StackStatus == cloudformation.StackStatusCreateComplete {
					req_ := cloudformationClient.ListStackResourcesRequest(&cloudformation.ListStackResourcesInput{StackName: i.StackName})
					res_, err := req_.Send(ctx)
					if err != nil {
						log.Println(err)
						break
					}
					for _, j := range res_.ListStackResourcesOutput.StackResourceSummaries {
						if aws.StringValue(j.ResourceType) == "AWS::ApiGatewayV2::Api" {
							url = "https://" + aws.StringValue(j.PhysicalResourceId) + ".execute-api." + os.Getenv("REGION") + ".amazonaws.com/"
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
		cloudformationClient = cloudformation.New(cfg)
	}
	req := cloudformationClient.CreateStackRequest(&cloudformation.CreateStackInput{
		Capabilities: []cloudformation.Capability{
			cloudformation.CapabilityCapabilityIam,
			cloudformation.CapabilityCapabilityAutoExpand,
		},
		StackName: aws.String(stackName),
		TemplateURL: aws.String(templateUrl),
	})
	_, err = req.Send(ctx)
	if err != nil {
		log.Println(err)
		return err
	}
	return nil
}

func deleteStack(ctx context.Context, name string) error {
	if cloudformationClient == nil {
		cloudformationClient = cloudformation.New(cfg)
	}
	req := cloudformationClient.DeleteStackRequest(&cloudformation.DeleteStackInput{
		StackName: aws.String(name),
	})
	_, err := req.Send(ctx)
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

func init() {
	var err error
	cfg, err = external.LoadDefaultAWSConfig()
	cfg.Region = os.Getenv("REGION")
	if err != nil {
		log.Print(err)
	}
}

func main() {
	lambda.Start(HandleRequest)
}
