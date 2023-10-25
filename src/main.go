package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"./src/config/conf"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/ct/studies"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/ct/units"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/ct/variables"
)

type EligibilityRelation struct {
	NctID           string                 `json:"nct_id"`
	EligibilityType string                 `json:"eligibility_type"`
	VariableType    string                 `json:"variable_type"`
	CriterionIndex  string                 `json:"criterion_index"`
	Criterion       string                 `json:"criterion"`
	Question        string                 `json:"question"`
	Relation        map[string]interface{} `json:"relation"`
}

// LambdaHandler is the Lambda function handler
func LambdaHandler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	fmt.Printf("Context: %+v\n", ctx)
	fmt.Printf("Event: %+v\n", event)
	rawBody := event.Body
	fmt.Println(rawBody)
	// Parse the input data from the Lambda event
	var data [][]string
	if err := json.Unmarshal([]byte(rawBody), &data); err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: "Invalid input data"}, nil
	}

	// Create and initialize the parser
	p := NewParser()
	fmt.Println("=============$$$$$$$$$$==============")
	if err := p.LoadParameters(); err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Failed to load parameters"}, nil
	}
	fmt.Println("************************")
	if err := p.Initialize(); err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Failed to initialize the parser"}, nil
	}

	// Ingest the data
	if err := p.Ingest(data); err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Failed to ingest data"}, nil
	}

	// Parse the data and get the result
	result := p.Parse()

	// Close the parser
	p.Close()

	// Convert the result to a JSON string
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Failed to marshal result to JSON"}, nil
	}

	// Return the result as an API Gateway response
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: string(jsonBytes)}, nil
}

// Parser defines the struct for processing eligibility criteria.
type Parser struct {
	parameters conf.Config
	registry   studies.Studies
}

// NewParser creates a new parser to parse eligibility criteria.
func NewParser() *Parser {
	return &Parser{}
}

// LoadParameters loads parameters from command line and a config file.
func (p *Parser) LoadParameters() error {
	// Print the working directory
	wd, err := os.Getwd()
	if err != nil {
		fmt.Println("Error getting working directory:", err)
	}
	fmt.Println("Working Directory:", wd)

	configfile := filepath.Join(wd, "resources", "config", "cfg.conf")
	fmt.Println("config path:", configfile)
	parameters, err := conf.Load(configfile)
	fmt.Println("ERROR::::", err)
	if err != nil {
		return err
	}
	p.parameters = parameters

	//p.parameters = make(map[string]string)
	//p.parameters["variable_file"] = "https://drive.google.com/file/d/1muXtDIN-e1btKd1_6-IYrAR_2ZKSj5Es/view?usp=sharing"
	//p.parameters["unit_file"] = "https://drive.google.com/file/d/1a9t7Y3kvn5LjqAqX3XZnrNSWs3NqSsqM/view?usp=sharing"

	return nil
}

// Initialize initializes the parser by loading the resource data.
func (p *Parser) Initialize() error {
	fname := p.parameters.GetResourcePath("variable_file")
	variableDictionary, err := variables.Load(fname)
	if err != nil {
		return err
	}
	variables.Set(variableDictionary)

	fname = p.parameters.GetResourcePath("unit_file")
	unitDictionary, err := units.Load(fname)
	if err != nil {
		return err
	}
	units.Set(unitDictionary)

	return nil
}

// Ingest allows ingesting eligibility criteria from a string slice.
func (p *Parser) Ingest(data [][]string) error {
	registry := studies.New()

	for _, row := range data {
		if len(row) < 5 {
			return fmt.Errorf("too few columns, at least 5 needed: %v", row)
		}

		nctID := row[0]
		title := row[1]
		conditions := strings.Split(row[3], "Exclusion Criteria: ")
		eligibilityCriteria := row[4]

		study := studies.NewStudy(nctID, title, conditions, eligibilityCriteria)
		registry.Add(study)
	}

	fmt.Printf("Ingested studies: %d\n", registry.Len())
	p.registry = registry

	return nil
}

// Parse parses the ingested eligibility criteria and writes the results to a file.
func (p *Parser) Parse() []EligibilityRelation {
	criteriaCnt := 0
	parsedCriteriaCnt := 0
	relationCnt := 0
	var allResults []EligibilityRelation
	for _, study := range p.registry {
		res := study.Parse().Relations()
		jsonResult, err := ParseInputString(res)
		if err != nil {
			panic(err)
		}
		for _, result := range jsonResult {
			resultMap := EligibilityRelation{
				NctID:           result.NctID,
				EligibilityType: result.EligibilityType,
				VariableType:    result.VariableType,
				CriterionIndex:  result.CriterionIndex,
				Criterion:       result.Criterion,
				Question:        result.Question,
				Relation:        result.Relation,
			}
			allResults = append(allResults, resultMap)
		}

		criteriaCnt += study.CriteriaCount()
		parsedCriteriaCnt += study.ParsedCriteriaCount()
		relationCnt += study.RelationCount()
	}

	ratio := 0.0
	if criteriaCnt > 0 {
		ratio = 100 * float64(relationCnt) / float64(criteriaCnt)
	}
	fmt.Printf("Ingested studies: %d, Extracted criteria: %d, Parsed criteria: %d, Relations: %d, Relations per criteria: %.1f%%\n",
		p.registry.Len(), criteriaCnt, parsedCriteriaCnt, relationCnt, ratio)

	return allResults
}

// Close closes the parser.
func (p *Parser) Close() {
	// Perform any cleanup here if needed
}

func ParseInputString(input string) ([]EligibilityRelation, error) {
	lines := strings.Split(input, "\n")
	result := []EligibilityRelation{}
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) == 7 {
			nct, relationType, variableType, cid, criteria, question, jsonStr := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6]
			var jsonMap map[string]interface{}
			if err := json.Unmarshal([]byte(jsonStr), &jsonMap); err != nil {
				return nil, err
			}

			relation := EligibilityRelation{
				NctID:           nct,
				EligibilityType: relationType,
				VariableType:    variableType,
				CriterionIndex:  cid,
				Criterion:       criteria,
				Question:        question,
				Relation:        jsonMap,
			}
			result = append(result, relation)
		}
	}

	return result, nil
}

func main() {
	lambda.Start(LambdaHandler)
}

//func main() {
//	// Create a mock event
//	data := [][]string{
//		{"NCT04343014", "Tongue Root Retractor For Fibroscopic Intubation", "false", "Airway Management", "Inclusion Criteria: - aged 18 to 70 years - ASA graded I~II class - scheduled for elective surgery requiring orotracheal intubation Exclusion Criteria: - with organ transplant operations - with thoracic and cardiac vascular surgery - with severe cardiac or pulmonary disease - BMI over 35kg/m2"},
//		{"NCT04342793", "A Study to Evaluate the Efficacy and Safety of ALS-L1023 in Subjects With NASH", "false", "Nonalcoholic Steatohepatitis", "Inclusion Criteria: - Men or women ages 19 and over, under 75 years of age - Patients diagnosed with NAFLD on abdominal ultrasonography and MRI - Patients show presence of hepatic fat fraction as defined by ≥ 8% on MRI-PDFF and liver stiffness as defined by ≥ 2.5 kPa on MRE at Screening Exclusion Criteria: - Any subject with current, significant alcohol consumption or a history of significant alcohol consumption for a period of more than 3 consecutive months any time within 2 year prior to screening will be excluded - Chronic liver disease (including hemochromatosis, liver cancer, autoimmune liver disease, viral hepatitis A, B, alcoholic liver disease - Uncontrolled diabetes mellitus as defined by a HbA1c ≥ 9.0％ at Screening - Patients who are allergic or hypersensitive to the drug or its constituents - Pregnant or lactating women"},
//	}
//	requestBody, err := json.Marshal(data)
//	if err != nil {
//		fmt.Println("Error creating request body:", err)
//		return
//	}
//	event := events.APIGatewayProxyRequest{
//		HTTPMethod: "POST",
//		Path:       "/parser",
//		Body:       string(requestBody),
//	}
//
//	// Call the handler function with the mock event
//	response, err := LambdaHandler(context.Background(), event)
//	if err != nil {
//		fmt.Println("Error:", err)
//		return
//	}
//
//	fmt.Println("Response:", response.Body)
//}
