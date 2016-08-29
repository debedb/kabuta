#include <stdio.h>
#include <pthread.h>

void threadFunc(int id) {
	while (1) {
		usleep(100);
		printf("threadFunc %d", id);
	}
}

int main(int argc, char** argv) {
	printf("Hello, world and");
	int i;
	for (i = 0; i < argc; i++) {
		if (i > 0) {
			printf(" ");
		}
		printf("%s", argv[i]);
	}
	printf("\n");
	pthread_t pth1, pth2;
	pthread_create(&pth, NULL, threadFunc, "processing...");
	pthread_create(&pth, NULL, threadFunc, "processing...");
	pthread_join(pth1, NULL);
	pthread_join(pth2, NULL);
}
