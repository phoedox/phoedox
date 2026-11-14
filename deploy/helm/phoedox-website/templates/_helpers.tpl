{{- define "website.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}{{ .Release.Name | trunc 53 | trimSuffix "-" }}
{{- else -}}{{ printf "%s-%s" .Release.Name .Chart.Name | trunc 53 | trimSuffix "-" }}{{- end -}}
{{- end -}}

{{- define "website.selector" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "website.labels" -}}
{{ include "website.selector" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}
