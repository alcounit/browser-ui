package types

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Session struct {
	SessionId      string          `json:"sessionId"`
	BrowserId      string          `json:"browserId"`
	BrowserIP      string          `json:"-"`
	BrowserName    string          `json:"browserName"`
	BrowserVersion string          `json:"browserVersion"`
	StartTime      *metav1.Time    `json:"startTime"`
	Phase          corev1.PodPhase `json:"phase"`
}
