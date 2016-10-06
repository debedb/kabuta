#include <stdio.h>
#include <pthread.h>

void threadFunc(char *arg) {
	int i = 0;
	while (1) {
		i++;
		usleep(1000000);
		printf("threadFunc iteration %d, thread %s\n", i, arg);
	}
}

int main(int argc, char** argv) {
	printf("%s says: hello, world and ", argv[0]);
	int i;
	for (i = 1; i < argc; i++) {
		if (i > 1) {
			printf(" ");
		}
		printf("%s", argv[i]);
	}
	printf("\n");
	pthread_t pth1, pth2;
	pthread_create(&pth1, NULL, threadFunc, "1");
	printf("Created %d\n", pth1);
	pthread_create(&pth2, NULL, threadFunc, "2");
	printf("Created %d\n", pth2);
	pthread_join(pth1, NULL);
	pthread_join(pth2, NULL);
}
