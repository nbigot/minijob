package constants

// Errors

const ErrorInvalidJobUuid = 1000
const ErrorJobUuidNotFound = 1001
const ErrorCantCreateJob = 1002
const ErrorCantDeleteJob = 1003
const ErrorCantPullAnyJob = 1004
const ErrorCantPullSpecificJob = 1005
const ErrorCantStartJob = 1006
const ErrorCantCloneJob = 1007
const ErrorCantCancelJob = 1008
const ErrorCantSetJobAsSuccessful = 1009
const ErrorCantFailJob = 1010
const ErrorCantEnqueueJob = 1011

const ErrorInvalidIteratorUuid = 1020
const ErrorInvalidParameterValue = 1021
const ErrorCantDeserializeJson = 1022
const ErrorInvalidJsonData = 1023
const ErrorInvalidContentType = 1024

const ErrorCantPutMessageIntoJob = 1110
const ErrorCantPutMessagesIntoJob = 1111
const ErrorCantDeserializeJsonRecords = 1112
const ErrorInvalidCreateRecordsIteratorRequest = 1113
const ErrorCantCreateRecordsIterator = 1114

const ErrorCantGetMessagesFromJob = 1220

const ErrorCantCloseStreamIterator = 1330
const ErrorStreamIteratorNotFound = 1331
const ErrorStreamIteratorIsBusy = 1332

const ErrorDatabaseUnavailable = 1440

const JobUuidParam = "jobuuid"
const NumJobsQueryParam = "numjobs"
const VisibilityTimeoutQueryParam = "visibilitytimeout"
const WaitTimeSecondsQueryParam = "waittimeseconds"
