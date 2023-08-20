# otel-transparency-collector

This repo is for the Custom OpenTelemetry Collector Distro integrated with the custom transparency processor. The builder manifest that declares which components (receivers, processors, exporters, and connectors) are available in this custom collector can be found in [builder.yaml](https://github.com/akkbng/otel-transparency-collector/blob/main/builder.yaml)

The custom transparency processor is an extension of the existing [tailsampling processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/tailsamplingprocessor) created by OpenTelemetry authors.  It enables sampling of the traces based on the accuracy of the transparency information coming from manually instrumented traces against the static privacy declarations.

The files regarding the configuration of the processor, processor metrics generation and the core tail sampling logic are taken from the tailsampling processor.

## Internal
This package contains some packages that were originally contributed to opentelemetry-collector-contrib under [./coreinternal](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/internal/coreinternal) and to tailsampling processor under [./internal](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/tailsamplingprocessor/internal).

Since the internal packages from other projects can’t be imported directly, they are copied under this project’s internal package.

### Sampling
This package consists of most of the sampling policies already available in the existing tailsampling processor, with the new [transparency attribute sampling policy](https://github.com/akkbng/otel-transparency-collector/blob/main/internal/sampling/transparency_tag_filter.go) added. 

## Deployment
This project has a built-in [CI/CD setup with GitHub Actions](https://github.com/akkbng/otel-transparency-collector/blob/main/.github/workflows/docker.yml) to build and publish the custom OpenTelemetry Collector Distro as a docker image. They are stored as public packages in Github Container Registry.

The custom collector image can be downloaded and installed in the development and production environments with the instructions specified on the [package page](https://github.com/akkbng/otel-transparency-collector/pkgs/container/otel-transparency-collector) of the project. 

### With Helm
The custom collector can also be deployed into Kubernetes cluster with a Helm chart. The modified Helm chart to deploy this custom collector can be found [here](https://github.com/akkbng/thesis-infrastructure/tree/main/charts/opentelemetry-collector). 
