// Copyright 2024 Intel Corp. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <netinet/ip.h>
#include <netinet/if_ether.h>
#include <netinet/tcp.h>
#include <netinet/udp.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <net/if.h>
#include <sys/ioctl.h>
#include <linux/if_packet.h>
#include <pthread.h>

struct ThreadArgs {
    int rawSocket1;
    int rawSocket2;
    struct sockaddr_ll sockAddr;  
    int threadID;      
};

void printPacket(char *packet, int length) {
    int i;
    for (i = 0; i < length; i++) {
        printf("%02x ", (unsigned char)packet[i]);
        if ((i + 1) % 16 == 0) {
            printf("\n");
        }
    }
    printf("\n");
}

void *socketThread(void *arg) {
    struct ThreadArgs *args = (struct ThreadArgs *)arg;
    int rawSocket1 = args->rawSocket1;
    int rawSocket2 = args->rawSocket2;
    struct sockaddr_ll sockAddr = args->sockAddr;  
    int threadID = args->threadID;    

    static int i = 0;  
    // Receive and send packets
    while (1) {
        char buffer[65536];
        ssize_t length;

        // Receive packet on port 1 
        length = recvfrom(rawSocket1, buffer, sizeof(buffer), 0, NULL, NULL);
        if (length == -1) {
            perror("Failed to receive packet on rx port");
            close(rawSocket1);
            exit(1);
        }
        else {    
	        i++;

            // Extract the Ethernet packet type
            unsigned short etherType = ntohs(*(unsigned short *)(buffer + 12));
            // Print the Ethernet packet type
            // printf("Ethernet Packet Type: 0x%04x\n", etherType);        

            printf("received frame on rx port %d. Sent to tx Port\n Total NF Frame count %d\n", threadID, i);    

            // Extract the source MAC address
            unsigned char *sourceMac = buffer + 6;
            // Print the source MAC address
            printf("Source MAC Address: %02x:%02x:%02x:%02x:%02x:%02x\n",
           sourceMac[0], sourceMac[1], sourceMac[2], sourceMac[3], sourceMac[4], sourceMac[5]);

           //Extract the Destination MAC address
            unsigned char *destinationMacAddress = buffer;
            // Print the source MAC address
            printf("Destination MAC Address: %02x:%02x:%02x:%02x:%02x:%02x\n",
           destinationMacAddress[0], destinationMacAddress[1], destinationMacAddress[2],
           destinationMacAddress[3], destinationMacAddress[4], destinationMacAddress[5]);     

            // Send packet to port2
            if (sendto(rawSocket2, buffer, length, 0, (struct sockaddr *)&sockAddr, sizeof(struct sockaddr_ll)) == -1) {
                perror("Failed to send packet on port tx");
                close(rawSocket2);
                exit(1);
            } 
        }
    }
     pthread_exit(NULL);
}   

int main() {
    int rawSocket1, rawSocket2;
    struct sockaddr_ll sock_addr1, sock_addr2;
    struct ifreq interface1, interface2;
    pthread_t thread1, thread2;

    struct ThreadArgs threadArgs1;
    struct ThreadArgs threadArgs2;

    // Create raw socket
    rawSocket1 = socket(AF_PACKET, SOCK_RAW, htons(ETH_P_ALL));
    if (rawSocket1 == -1) {
        perror("Failed to create raw Socket_fd1");
        exit(1);
    }

    rawSocket2 = socket(AF_PACKET, SOCK_RAW, htons(ETH_P_ALL));
    if (rawSocket2 == -1) {
        perror("Failed to create raw Socket_fd2");
        exit(1);
    }

    // Connect to interface 1
    strncpy(interface1.ifr_name, "net1", IFNAMSIZ);
    if (ioctl(rawSocket1, SIOCGIFINDEX, &interface1) == -1) {
        perror("Failed to get interface index tap 1");
        close(rawSocket1);
        exit(1);
    }
    // Connect to interface 2
    strncpy(interface2.ifr_name, "net2", IFNAMSIZ);
    if (ioctl(rawSocket2, SIOCGIFINDEX, &interface2) == -1) {
        perror("Failed to get interface index");
        close(rawSocket2);
        exit(1);
    }
    // Bind socket to interface 1
    memset(&sock_addr1, 0, sizeof(struct sockaddr_ll));
    sock_addr1.sll_family = AF_PACKET;
    sock_addr1.sll_protocol = htons(ETH_P_ALL);
    sock_addr1.sll_ifindex = interface1.ifr_ifindex;
    if (bind(rawSocket1, (struct sockaddr *)&sock_addr1, sizeof(struct sockaddr_ll)) == -1) {
        perror("Failed to bind raw socket to interface");
        close(rawSocket1);
        exit(1);
    }
    printf("the value of sock_addr1 is %u", sock_addr1);


    //  Bind socket to interface 2
    memset(&sock_addr2, 0, sizeof(struct sockaddr_ll));
    sock_addr2.sll_family = AF_PACKET;
    sock_addr2.sll_protocol = htons(ETH_P_ALL);
    sock_addr2.sll_ifindex = interface2.ifr_ifindex;
    static int i = 0;
    if (bind(rawSocket2, (struct sockaddr *)&sock_addr2, sizeof(struct sockaddr_ll)) == -1) {
        perror("Failed to bind raw socket to interface");
        close(rawSocket2);
        exit(1);
    }

    printf("the value of sock_addr2 is %u", sock_addr2);

    threadArgs1.rawSocket1 = rawSocket1;
    threadArgs1.rawSocket2 = rawSocket2;  
    threadArgs1.sockAddr = sock_addr2;  
    threadArgs1.threadID = 1;

    threadArgs2.rawSocket1 = rawSocket2;
    threadArgs2.rawSocket2 = rawSocket1;  
    threadArgs2.sockAddr = sock_addr1;    
    threadArgs2.threadID = 2;

    pthread_create(&thread1, NULL, socketThread, &threadArgs1);
    pthread_create(&thread2, NULL, socketThread, &threadArgs2);
    // Wait for the threads to finish
    pthread_join(thread1, NULL);
    pthread_join(thread2, NULL);
    // Close socket
    close(rawSocket1);
    close(rawSocket2);
    return 0;    
}
