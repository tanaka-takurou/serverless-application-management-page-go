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
		serverlessApplicationRepositoryClient = getSARClient(ctx)
	}
	res, err := serverlessApplicationRepositoryClient.ListApplications(ctx, &serverlessapplicationrepository.ListApplicationsInput{})
	if err != nil {
		return nil, err
	}
	var applicationList []Application
	for _, i := range res.Applications {
		applicationList = append(applicationList, Application{
			Name:        aws.ToString(i.Name),
			Description: aws.ToString(i.Description),
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
		serverlessApplicationRepositoryClient = getSARClient(ctx)
	}
	res, err := serverlessApplicationRepositoryClient.ListApplications(ctx, &serverlessapplicationrepository.ListApplicationsInput{})
	if err != nil {
		return "", err
	}
	var applicationId string
	for _, i := range res.Applications {
		if name == aws.ToString(i.Name) {
			applicationId = aws.ToString(i.ApplicationId)
			break
		}
	}
	return applicationId, nil
}

func getTemplateUrl(ctx context.Context, applicationId string)(string, error) {
	if serverlessApplicationRepositoryClient == nil {
		serverlessApplicationRepositoryClient = getSARClient(ctx)
	}
	res, err := serverlessApplicationRepositoryClient.CreateCloudFormationTemplate(ctx, &serverlessapplicationrepository.CreateCloudFormationTemplateInput{
		ApplicationId: aws.String(applicationId),
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(res.TemplateUrl), nil
}

func addStackData(ctx context.Context, applicationList []Application)([]Application, error) {
	if cloudformationClient == nil {
		cloudformationClient = getCloudformationClient(ctx)
	}
	res, err := cloudformationClient.ListStacks(ctx, &cloudformation.ListStacksInput{
		StackStatusFilter: []types.StackStatus{
			types.StackStatusCreateComplete,
			types.StackStatusCreateInProgress,
			types.StackStatusDeleteInProgress,
		},
	})
	if err != nil {
		return nil, err
	}
	for _, i := range res.StackSummaries {
		stackName := aws.ToString(i.StackName)
		for n, j := range applicationList {
			if strings.HasPrefix(stackName, j.Name) {
				var url string
				if i.StackStatus == types.StackStatusCreateComplete{
					res_, err := cloudformationClient.ListStackResources(ctx, &cloudformation.ListStackResourcesInput{StackName: i.StackName})
					if err != nil {
						log.Println(err)
						break
					}
					for _, j := range res_.StackResourceSummaries {
						if aws.ToString(j.ResourceType) == "AWS::ApiGatewayV2::Api" {
							url = "https://" + aws.ToString(j.PhysicalResourceId) + ".execute-api." + os.Getenv("REGION") + ".amazonaws.com/"
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
		cloudformationClient = getCloudformationClient(ctx)
	}
	_, err = cloudformationClient.CreateStack(ctx, &cloudformation.CreateStackInput{
		Capabilities: []types.Capability{
			types.CapabilityCapabilityIam,
			types.CapabilityCapabilityAutoExpand,
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
		cloudformationClient = getCloudformationClient(ctx)
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

func getSARClient(ctx context.Context) *serverlessapplicationrepository.Client {
	return serverlessapplicationrepository.NewFromConfig(getConfig(ctx))
}

func getCloudformationClient(ctx context.Context) *cloudformation.Client {
	return cloudformation.NewFromConfig(getConfig(ctx))
}

func getConfig(ctx context.Context) aws.Config {
	var err error
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(os.Getenv("REGION")))
	if err != nil {
		log.Print(err)
	}
	return cfg
}

func main() {
	lambda.Start(HandleRequest)
}
